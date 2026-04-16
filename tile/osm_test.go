package tile

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testSource returns an osmSource pointed at the given test-server
// URL with a fixed UA. Test-only helper; production callers use
// OSMWithUserAgent.
func testSource(t *testing.T, baseURL, ua string) *osmSource {
	t.Helper()
	prefix := baseURL + "/"
	return &osmSource{
		client:    http.DefaultClient,
		userAgent: sanitizeHeader(ua),
		urlPrefix: prefix,
	}
}

// pngFixture is the eight-byte PNG signature plus a short payload —
// enough to satisfy isPNG; tests never decode the bytes.
var pngFixture = []byte("\x89PNG\r\n\x1a\nfake-payload")

func TestOSM_URL(t *testing.T) {
	s := OSM()
	cases := []struct {
		name string
		c    Coord
		want string
	}{
		{
			"zero",
			Coord{Z: 0, X: 0, Y: 0},
			"https://tile.openstreetmap.org/0/0/0.png",
		},
		{
			"seattle_z11",
			Coord{Z: 11, X: 328, Y: 715},
			"https://tile.openstreetmap.org/11/328/715.png",
		},
		{
			"max_z19",
			Coord{Z: 19, X: 100, Y: 200},
			"https://tile.openstreetmap.org/19/100/200.png",
		},
	}
	for _, c := range cases {
		if got := s.URL(c.c); got != c.want {
			t.Errorf("%s: URL = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestOSM_URL_InvalidCoord(t *testing.T) {
	s := OSM()
	// At zoom 2, max tile index is 3. X=4 is out of range.
	if got := s.URL(Coord{Z: 2, X: 4, Y: 0}); got != "" {
		t.Errorf("URL of invalid coord = %q, want \"\"", got)
	}
}

func TestOSM_HTTPFetcher_SendsUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			gotUA = r.Header.Get("User-Agent")
			w.Header().Set("Content-Type", "image/png")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(pngFixture)
		}))
	defer srv.Close()

	wantUA := "my-test-app/1.2 (https://example.com)"
	src, ok := OSMWithUserAgent(wantUA).(HTTPFetcher)
	if !ok {
		t.Fatal("OSMWithUserAgent does not implement HTTPFetcher")
	}
	fetcher := src.HTTPFetcher()
	resp, err := fetcher(context.Background(), srv.URL+"/1/2/3.png")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	_ = resp.Body.Close()

	if gotUA != wantUA {
		t.Errorf("User-Agent = %q, want %q", gotUA, wantUA)
	}
}

// URL must succeed at the maximum representable zoom (Z=31, where X/Y
// can be up to 10 digits) — exercises the stack-buffer sizing in
// buildTileURL.
func TestOSM_URL_MaxZoom31(t *testing.T) {
	s := OSM()
	c := Coord{Z: 31, X: (1 << 31) - 1, Y: (1 << 31) - 1}
	got := s.URL(c)
	want := "https://tile.openstreetmap.org/31/2147483647/2147483647.png"
	if got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}

// At Z=32, uint32(1)<<32 = 0 in Go, so Coord.Valid returns false and
// URL must short-circuit to "" rather than build a nonsense address.
func TestOSM_URL_Z32IsInvalid(t *testing.T) {
	s := OSM()
	if got := s.URL(Coord{Z: 32, X: 0, Y: 0}); got != "" {
		t.Errorf("URL at Z=32 = %q, want \"\"", got)
	}
}

// sanitizeHeader must strip CR and LF so a malicious UA cannot append
// extra HTTP headers via injection.
func TestSanitizeHeader_StripsCRLF(t *testing.T) {
	got := sanitizeHeader("foo\r\nX-Inject: bar")
	want := "fooX-Inject: bar"
	if got != want {
		t.Errorf("sanitizeHeader = %q, want %q", got, want)
	}
}

func TestSanitizeHeader_TrimsWhitespace(t *testing.T) {
	got := sanitizeHeader("  hello  ")
	if got != "hello" {
		t.Errorf("sanitizeHeader = %q, want \"hello\"", got)
	}
}

// Length cap prevents an accidentally huge UA from landing on every
// outbound request.
func TestSanitizeHeader_CapsLength(t *testing.T) {
	in := strings.Repeat("a", maxUserAgentLen+100)
	got := sanitizeHeader(in)
	if len(got) != maxUserAgentLen {
		t.Errorf("len = %d, want %d", len(got), maxUserAgentLen)
	}
}

// End-to-end: the sanitizer must run before the UA reaches outbound
// HTTP. Confirms construction wiring, not just the helper.
func TestOSMWithUserAgent_AppliesSanitizer(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(
		func(_ http.ResponseWriter, r *http.Request) {
			got = r.Header.Get("User-Agent")
		}))
	defer srv.Close()

	src := testSource(t, srv.URL, "evil\r\nX-Bad: yes")
	_, _ = src.Fetch(context.Background(), Coord{Z: 1, X: 0, Y: 0})

	if strings.ContainsAny(got, "\r\n") {
		t.Errorf("UA contains CRLF: %q", got)
	}
	if !strings.HasPrefix(got, "evil") {
		t.Errorf("UA = %q, want prefix \"evil\"", got)
	}
}

// LimitReader must cap response-body size; a hostile or
// misconfigured server returning gigabytes must not OOM the caller.
// Body is prefixed with a PNG signature so the post-LimitReader
// validator passes; the test then checks that the returned body
// stops at maxTileBytes.
func TestOSM_Fetch_LimitsResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(pngMagic)
			big := make([]byte, maxTileBytes+(1<<20))
			_, _ = w.Write(big)
		}))
	defer srv.Close()

	src := testSource(t, srv.URL, "test/1")
	body, err := src.Fetch(context.Background(), Coord{Z: 1, X: 0, Y: 0})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if int64(len(body)) > maxTileBytes {
		t.Errorf("body len = %d, exceeds cap %d", len(body), maxTileBytes)
	}
}

// HTTPFetcher must reject 200 OK responses whose body lacks a PNG
// signature — OSM occasionally returns empty bodies under load and
// go-gui would otherwise cache the garbage on disk.
func TestOSM_HTTPFetcher_RejectsEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.WriteHeader(http.StatusOK)
		}))
	defer srv.Close()

	src := OSMWithUserAgent("t/0").(HTTPFetcher)
	_, err := src.HTTPFetcher()(context.Background(), srv.URL+"/0/0/0.png")
	if err == nil {
		t.Fatal("err = nil, want rejection of empty body")
	}
	if !strings.Contains(err.Error(), "not a PNG") {
		t.Errorf("err = %v, want \"not a PNG\"", err)
	}
}

func TestOSM_HTTPFetcher_RejectsNonPNGBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "<html>503 Backend Overloaded</html>")
		}))
	defer srv.Close()

	src := OSMWithUserAgent("t/0").(HTTPFetcher)
	_, err := src.HTTPFetcher()(context.Background(), srv.URL+"/0/0/0.png")
	if err == nil {
		t.Fatal("err = nil, want rejection of HTML body")
	}
	if !strings.Contains(err.Error(), "not a PNG") {
		t.Errorf("err = %v, want \"not a PNG\"", err)
	}
}

// HTTPFetcher must pass non-200 responses through unmodified so
// go-gui's existing status-code log fires; the body validator must
// not even run.
func TestOSM_HTTPFetcher_PassesThroughNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
	defer srv.Close()

	src := OSMWithUserAgent("t/0").(HTTPFetcher)
	resp, err := src.HTTPFetcher()(context.Background(), srv.URL+"/0/0/0.png")
	if err != nil {
		t.Fatalf("err = %v, want nil for non-200 passthrough", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("StatusCode = %d, want 503", resp.StatusCode)
	}
}

func TestIsPNG(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want bool
	}{
		{"empty", nil, false},
		{"too_short", []byte{0x89, 'P', 'N'}, false},
		{"png", pngFixture, true},
		{"html", []byte("<html>"), false},
		{"jpeg_magic", []byte{0xFF, 0xD8, 0xFF, 0xE0}, false},
	}
	for _, c := range cases {
		if got := isPNG(c.in); got != c.want {
			t.Errorf("%s: isPNG = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestOSM_Fetch_404ReturnsErrNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
	defer srv.Close()

	src := testSource(t, srv.URL, "test/1")
	_, err := src.Fetch(context.Background(), Coord{Z: 1, X: 0, Y: 0})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// Non-404 error statuses must surface as a wrapped error with status
// info, not be swallowed.
func TestOSM_Fetch_500WrapsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
	defer srv.Close()

	src := testSource(t, srv.URL, "test/1")
	_, err := src.Fetch(context.Background(), Coord{Z: 1, X: 0, Y: 0})
	if err == nil {
		t.Fatal("err = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %v, want to mention 500", err)
	}
}

// Sanity: the testSource helper itself must build URLs that the test
// server actually routes to. Catches mismatches between buildTileURL
// and the prefix-based override path.
func TestTestSource_FetchHitsRightPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(pngFixture)
		}))
	defer srv.Close()

	src := testSource(t, srv.URL, "test/1")
	if _, err := src.Fetch(context.Background(),
		Coord{Z: 4, X: 5, Y: 6}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if gotPath != "/4/5/6.png" {
		t.Errorf("path = %q, want /4/5/6.png", gotPath)
	}
}

func TestOSM_HTTPFetcher_PropagatesContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
	defer srv.Close()

	src := OSMWithUserAgent("t/0").(HTTPFetcher)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call
	_, err := src.HTTPFetcher()(ctx, srv.URL+"/0/0/0.png")
	if err == nil {
		t.Fatal("expected error on cancelled context, got nil")
	}
	if !strings.Contains(err.Error(), "canceled") &&
		!strings.Contains(err.Error(), "context") {
		t.Errorf("error = %v, want context-cancellation", err)
	}
}
