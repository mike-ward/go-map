package tile

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// osmSource fetches tiles from the public OSM tile server.
// Usage requires compliance with
// https://operations.osmfoundation.org/policies/tiles/
type osmSource struct {
	client    *http.Client
	userAgent string
	urlTpl    string
}

// OSM returns a tile source backed by the public OpenStreetMap tile
// server. The caller must set a descriptive User-Agent via
// OSMWithUserAgent for production use; heavy/anonymous traffic is
// blocked by OSM policy.
func OSM() Source {
	return OSMWithUserAgent("go-map/0 (https://github.com/mike-ward/go-map)")
}

// OSMWithUserAgent returns an OSM tile source using the given
// User-Agent string.
func OSMWithUserAgent(ua string) Source {
	return &osmSource{
		client:    &http.Client{Timeout: 15 * time.Second},
		userAgent: ua,
		urlTpl:    "https://tile.openstreetmap.org/%d/%d/%d.png",
	}
}

func (s *osmSource) URL(c Coord) string {
	if !c.Valid() {
		return ""
	}
	return fmt.Sprintf(s.urlTpl, c.Z, c.X, c.Y)
}

func (s *osmSource) Fetch(ctx context.Context, c Coord) ([]byte, error) {
	if !c.Valid() {
		return nil, ErrNotFound
	}
	url := fmt.Sprintf(s.urlTpl, c.Z, c.X, c.Y)
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
		return io.ReadAll(resp.Body)
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
// gui.WindowCfg.ImageFetcher. It sends the Source's User-Agent on
// every request — required by OSM tile policy when rendering via
// gui.DrawContext.Image. Sources that are not HTTP-backed return
// nil; the caller falls back to go-gui's default fetcher.
func (s *osmSource) HTTPFetcher() func(ctx context.Context, url string) (*http.Response, error) {
	return func(ctx context.Context, url string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", s.userAgent)
		return s.client.Do(req)
	}
}

// HTTPFetcher is implemented by Sources that speak HTTP and can
// supply a policy-compliant fetcher for gui.WindowCfg.ImageFetcher.
// Consumers type-assert to this interface.
type HTTPFetcher interface {
	HTTPFetcher() func(ctx context.Context, url string) (*http.Response, error)
}
