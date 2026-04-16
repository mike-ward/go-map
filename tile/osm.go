package tile

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// osmSource fetches tiles from the public OSM tile server.
// Usage requires compliance with
// https://operations.osmfoundation.org/policies/tiles/
//
// Throttling is the caller's responsibility: OSM tile policy bans
// heavy or bulk traffic. This source does not rate-limit.
type osmSource struct {
	client    *http.Client
	userAgent string
	urlPrefix string // per-server URL stem; overridable in tests.
}

// osmURLPrefix is the production OSM tile-server URL stem.
const osmURLPrefix = "https://tile.openstreetmap.org/"

// maxTileBytes caps response-body size per fetch. A 256² tile is a
// few KiB; the cap stops a hostile or misconfigured server from
// driving io.ReadAll into unbounded memory.
const maxTileBytes int64 = 4 << 20 // 4 MiB

// OSM returns a tile source backed by the public OpenStreetMap tile
// server. The caller must set a descriptive User-Agent via
// OSMWithUserAgent for production use; heavy/anonymous traffic is
// blocked by OSM policy.
func OSM() Source {
	return OSMWithUserAgent("go-map/0 (https://github.com/mike-ward/go-map)")
}

// OSMWithUserAgent returns an OSM tile source using the given
// User-Agent string. CR and LF are stripped to prevent header
// injection at write time (net/http would otherwise reject the
// request at runtime).
func OSMWithUserAgent(ua string) Source {
	return &osmSource{
		client:    &http.Client{Timeout: 15 * time.Second},
		userAgent: sanitizeHeader(ua),
		urlPrefix: osmURLPrefix,
	}
}

// maxUserAgentLen caps the length of the User-Agent we will set on
// outbound requests. RFC 7230 doesn't fix a header-value ceiling but
// many origins (and Go's own http2 stack) reject very long values;
// keep us defensive against accidental megabyte strings.
const maxUserAgentLen = 512

// sanitizeHeader strips characters that would make net/http reject a
// header value (\r, \n), trims surrounding whitespace, and caps the
// length so a hostile or malformed caller cannot inject an arbitrary
// blob into every outbound request.
func sanitizeHeader(s string) string {
	r := strings.NewReplacer("\r", "", "\n", "")
	out := strings.TrimSpace(r.Replace(s))
	if len(out) > maxUserAgentLen {
		out = out[:maxUserAgentLen]
	}
	return out
}

// buildTileURL composes "{prefix}{z}/{x}/{y}.png" without going
// through fmt: one allocation for the returned string vs ~5 for
// fmt.Sprintf, on every visible tile every frame.
func buildTileURL(prefix string, c Coord) string {
	var buf [128]byte
	b := append(buf[:0], prefix...)
	b = strconv.AppendUint(b, uint64(c.Z), 10)
	b = append(b, '/')
	b = strconv.AppendUint(b, uint64(c.X), 10)
	b = append(b, '/')
	b = strconv.AppendUint(b, uint64(c.Y), 10)
	b = append(b, ".png"...)
	return string(b)
}

func (s *osmSource) URL(c Coord) string {
	if !c.Valid() {
		return ""
	}
	return buildTileURL(s.urlPrefix, c)
}

func (s *osmSource) Fetch(ctx context.Context, c Coord) ([]byte, error) {
	if !c.Valid() {
		return nil, ErrNotFound
	}
	url := buildTileURL(s.urlPrefix, c)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.userAgent)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxTileBytes))
		if err != nil {
			return nil, err
		}
		if !isPNG(body) {
			return nil, fmt.Errorf(
				"tile %s: %d-byte body is not a PNG", url, len(body))
		}
		return body, nil
	case http.StatusNotFound:
		return nil, ErrNotFound
	default:
		return nil, fmt.Errorf("tile: %s: %s", url, resp.Status)
	}
}

func (s *osmSource) Attribution() string {
	return "© OpenStreetMap contributors"
}

func (*osmSource) MaxZoom() uint32 { return 19 }

// HTTPFetcher returns a function suitable for
// gui.WindowCfg.ImageFetcher. Sends the Source's User-Agent on every
// request — required by OSM tile policy when rendering via
// gui.DrawContext.Image.
//
// On 200 OK the body is read fully (cap maxTileBytes) and validated
// against a PNG signature before the response is returned. OSM
// intermittently answers 200 with an empty or HTML body under load;
// without this check go-gui would cache that garbage on disk and
// every subsequent frame would log "decode image: unknown format".
// Reading the body up front is cheap because tiles are small (≤ ~100
// KiB); the in-memory buffer is then wrapped in a NopCloser so the
// downstream io.Copy still streams.
func (s *osmSource) HTTPFetcher() func(ctx context.Context, url string) (*http.Response, error) {
	return func(ctx context.Context, url string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", s.userAgent)
		resp, err := s.client.Do(req)
		if err != nil {
			return nil, err
		}
		// Non-OK statuses pass through so go-gui's status-code log fires.
		if resp.StatusCode != http.StatusOK {
			return resp, nil
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxTileBytes))
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("tile body: %w", err)
		}
		if !isPNG(body) {
			return nil, fmt.Errorf(
				"tile %s: %d-byte body is not a PNG", url, len(body))
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.ContentLength = int64(len(body))
		return resp, nil
	}
}

// pngMagic is the eight-byte PNG file signature (RFC 2083 §3.1).
var pngMagic = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// isPNG reports whether b begins with the PNG file signature. A
// zero-length or HTML-error body fails the check; a real PNG passes
// even before the IDAT chunks are inspected.
func isPNG(b []byte) bool {
	return len(b) >= len(pngMagic) && bytes.Equal(b[:len(pngMagic)], pngMagic)
}

// HTTPFetcher is implemented by Sources that speak HTTP and can
// supply a policy-compliant fetcher for gui.WindowCfg.ImageFetcher.
// Consumers type-assert to this interface.
type HTTPFetcher interface {
	HTTPFetcher() func(ctx context.Context, url string) (*http.Response, error)
}
