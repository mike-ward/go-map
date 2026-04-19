// Package wms provides a WMS 1.3.0 tile.Source targeting the EPSG:3857
// web-mercator CRS. Other CRS and WMS 1.1.1 are out of scope: the axis-
// order rules differ per version and per CRS, and supporting them
// would widen the surface without a user to drive the choices.
//
// Typical use:
//
//	src, err := wms.New(wms.Cfg{
//	    Endpoint:    "https://ows.example.com/wms",
//	    Layers:      []string{"roads"},
//	    Attribution: "© Example WMS",
//	    MaxZoom:     18,
//	    Transparent: true, // overlay reference layer
//	})
//
// The returned Source also implements tile.HTTPFetcher. mapview pulls
// it automatically via DrawContext.ImageWithFetcher (go-gui v0.12.4+),
// so a WMS layer stacked alongside OSM carries its own User-Agent and
// body validator without the consumer wiring gui.WindowCfg.ImageFetcher.
package wms

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mike-ward/go-map/projection"
	"github.com/mike-ward/go-map/tile"
)

// mercatorR is half the width of the EPSG:3857 world, in meters. Every
// major WMS server uses this value for the 3857 / 900913 extent.
const mercatorR = 20037508.342789244

// maxBodyBytes caps WMS response reads. 8 MiB leaves headroom for
// high-DPI PNG-with-alpha tiles while still bounding memory against a
// hostile or misconfigured origin.
const maxBodyBytes int64 = 8 << 20

// defaultUA identifies the client when Cfg.UserAgent is empty. Some
// WMS deployments (USGS, GeoServer with throttling) require a
// non-empty UA.
const defaultUA = "go-map/0 (https://github.com/mike-ward/go-map)"

// tileEdge is the slippy tile pixel edge length, mirrored from
// projection.TileSize so the WMS WIDTH/HEIGHT match the tile grid.
const tileEdge = projection.TileSize

// Cfg parameterizes a WMS 1.3.0 source. Endpoint, Layers, Attribution,
// and MaxZoom are required; the factory returns an error otherwise.
type Cfg struct {
	// Endpoint is the base WMS URL. May already contain a query
	// string (e.g. "?map=foo.map" for MapServer); parameters are
	// appended with the correct separator.
	Endpoint string

	// Layers names the WMS layers to request, joined into the LAYERS
	// parameter in order. Each entry is URL-encoded.
	Layers []string

	// Styles aligns with Layers: Styles[i] applies to Layers[i]. Short
	// or nil slice pads with empty strings so STYLES has one slot per
	// layer — WMS 1.3.0 requires the parameter even when every slot
	// is the server default.
	Styles []string

	// Format is the requested MIME type. Defaults to "image/png". The
	// body validator accepts only image/png and image/jpeg; other
	// formats will fail even on a healthy 200 OK.
	Format string

	// Version pins the WMS protocol. Defaults to "1.3.0" — the only
	// version this package supports. Setting a different value still
	// sends it verbatim; behavior with 1.1.x is not guaranteed.
	Version string

	// Transparent sends TRANSPARENT=TRUE when true. Zero value is
	// false (opaque); overlay reference layers should set true
	// explicitly.
	Transparent bool

	// Attribution is the credit string rendered by the map widget.
	// WMS providers universally require credit; empty is rejected.
	Attribution string

	// MaxZoom is the highest slippy zoom this source serves. WMS has
	// no intrinsic tile pyramid, but callers typically cap to match
	// their data's usable detail.
	MaxZoom uint32

	// UserAgent is sent on every request, sanitized via
	// tile.SanitizeHeader. Empty falls back to defaultUA.
	UserAgent string
}

// source implements tile.Source + tile.HTTPFetcher for a pre-built URL
// template. All per-request query parameters except BBOX are baked into
// urlPrefix so URL(Coord) is one Sprintf-free concatenation.
type source struct {
	client      *http.Client
	userAgent   string
	urlPrefix   string // endpoint + fixed params + "&bbox="
	format      string
	attribution string
	maxZoom     uint32
	acceptPNG   bool
	acceptJPEG  bool
}

// New returns a WMS tile.Source, or an error when Cfg is incomplete or
// Endpoint is malformed.
func New(cfg Cfg) (tile.Source, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("wms: Endpoint required")
	}
	if len(cfg.Layers) == 0 {
		return nil, fmt.Errorf("wms: Layers required")
	}
	for i, name := range cfg.Layers {
		if name == "" {
			return nil, fmt.Errorf(
				"wms: Layers[%d] is empty (produces ambiguous LAYERS=)", i)
		}
	}
	if cfg.Attribution == "" {
		return nil, fmt.Errorf("wms: Attribution required")
	}
	if cfg.MaxZoom == 0 {
		return nil, fmt.Errorf("wms: MaxZoom required")
	}
	if _, err := url.Parse(cfg.Endpoint); err != nil {
		return nil, fmt.Errorf("wms: Endpoint: %w", err)
	}

	format := cfg.Format
	if format == "" {
		format = "image/png"
	}
	// Reject formats the body validator cannot check. A source that
	// always fails acceptBody is a silent footgun — prefer construction-
	// time error over every Fetch returning "not a png image".
	if format != "image/png" && format != "image/jpeg" && format != "image/jpg" {
		return nil, fmt.Errorf(
			"wms: Format %q unsupported (image/png, image/jpeg only)", format)
	}
	version := cfg.Version
	if version == "" {
		version = "1.3.0"
	}
	ua := cfg.UserAgent
	if ua == "" {
		ua = defaultUA
	}

	var buf strings.Builder
	buf.Grow(len(cfg.Endpoint) + 256)
	buf.WriteString(cfg.Endpoint)
	buf.WriteString(querySep(cfg.Endpoint))
	buf.WriteString("service=WMS&request=GetMap&version=")
	buf.WriteString(url.QueryEscape(version))
	buf.WriteString("&layers=")
	buf.WriteString(encodeCSV(cfg.Layers))
	buf.WriteString("&styles=")
	buf.WriteString(encodeStyles(cfg.Layers, cfg.Styles))
	buf.WriteString("&crs=EPSG:3857")
	buf.WriteString("&width=")
	buf.WriteString(strconv.Itoa(tileEdge))
	buf.WriteString("&height=")
	buf.WriteString(strconv.Itoa(tileEdge))
	buf.WriteString("&format=")
	buf.WriteString(url.QueryEscape(format))
	buf.WriteString("&transparent=")
	if cfg.Transparent {
		buf.WriteString("TRUE")
	} else {
		buf.WriteString("FALSE")
	}
	buf.WriteString("&bbox=")

	return &source{
		// 20 s — WMS servers do server-side rendering before responding,
		// which is slower than a simple tile-file serve (OSM uses 15 s).
		client: &http.Client{Timeout: 20 * time.Second},
		userAgent:   tile.SanitizeHeader(ua),
		urlPrefix:   buf.String(),
		format:      format,
		attribution: cfg.Attribution,
		maxZoom:     cfg.MaxZoom,
		acceptPNG:   format == "image/png",
		acceptJPEG:  format == "image/jpeg" || format == "image/jpg",
	}, nil
}

// querySep returns the separator required to append the first WMS
// parameter: "?" for endpoints with no query, "&" when one already
// exists, empty when the endpoint already ends with a separator.
func querySep(endpoint string) string {
	if !strings.Contains(endpoint, "?") {
		return "?"
	}
	if strings.HasSuffix(endpoint, "?") || strings.HasSuffix(endpoint, "&") {
		return ""
	}
	return "&"
}

// encodeCSV URL-encodes each element and joins with literal commas.
// Commas between list entries stay unencoded per WMS spec for LAYERS
// and STYLES.
func encodeCSV(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = url.QueryEscape(x)
	}
	return strings.Join(parts, ",")
}

// encodeStyles pads styles to match len(layers) with URL-encoded
// values, empty-string slots denoting the server-default style.
func encodeStyles(layers, styles []string) string {
	parts := make([]string, len(layers))
	for i := range layers {
		if i < len(styles) {
			parts[i] = url.QueryEscape(styles[i])
		}
	}
	return strings.Join(parts, ",")
}

// bboxFor returns the EPSG:3857 tile extent in "minx,miny,maxx,maxy"
// order. WMS 1.3.0 axis order for EPSG:3857 is easting,northing, which
// matches the slippy convention directly. strconv.AppendFloat with
// precision -1 produces the shortest round-trippable representation —
// a server-side parse reproduces the tile corner exactly.
func bboxFor(c tile.Coord) string {
	size := 2 * mercatorR / float64(uint64(1)<<c.Z)
	minX := -mercatorR + float64(c.X)*size
	maxX := minX + size
	maxY := mercatorR - float64(c.Y)*size
	minY := maxY - size

	var b [96]byte
	out := b[:0]
	out = strconv.AppendFloat(out, minX, 'f', -1, 64)
	out = append(out, ',')
	out = strconv.AppendFloat(out, minY, 'f', -1, 64)
	out = append(out, ',')
	out = strconv.AppendFloat(out, maxX, 'f', -1, 64)
	out = append(out, ',')
	out = strconv.AppendFloat(out, maxY, 'f', -1, 64)
	return string(out)
}

func (s *source) URL(c tile.Coord) string {
	if !c.Valid() {
		return ""
	}
	return s.urlPrefix + bboxFor(c)
}

func (s *source) Fetch(ctx context.Context, c tile.Coord) ([]byte, error) {
	if !c.Valid() {
		return nil, tile.ErrNotFound
	}
	u := s.URL(c)
	resp, err := s.do(ctx, u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		if err != nil {
			return nil, err
		}
		if !s.acceptBody(body) {
			return nil, fmt.Errorf(
				"wms %s: %d-byte body is not a %s image",
				u, len(body), s.format)
		}
		return body, nil
	case http.StatusNotFound:
		return nil, tile.ErrNotFound
	default:
		return nil, fmt.Errorf("wms: %s: %s", u, resp.Status)
	}
}

func (s *source) do(ctx context.Context, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.userAgent)
	return s.client.Do(req)
}

// acceptBody validates body against the magic bytes for the configured
// Format. A ServiceException XML error (text/xml 200 OK) or empty body
// fails both checks and is rejected so go-gui does not cache garbage.
func (s *source) acceptBody(body []byte) bool {
	if s.acceptPNG && tile.IsPNG(body) {
		return true
	}
	if s.acceptJPEG && tile.IsJPEG(body) {
		return true
	}
	return false
}

func (s *source) Attribution() string { return s.attribution }

func (s *source) MaxZoom() uint32 { return s.maxZoom }

// HTTPFetcher returns a fetcher suitable for gui.WindowCfg.ImageFetcher.
// Sends the source's User-Agent and validates 200 OK bodies before
// returning them, so go-gui's on-disk image cache never persists an
// error payload that would log "decode image: unknown format" every
// subsequent frame.
func (s *source) HTTPFetcher() func(ctx context.Context, url string) (*http.Response, error) {
	return func(ctx context.Context, u string) (*http.Response, error) {
		resp, err := s.do(ctx, u)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return resp, nil
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("wms body: %w", err)
		}
		if !s.acceptBody(body) {
			return nil, fmt.Errorf(
				"wms %s: %d-byte body is not a %s image",
				u, len(body), s.format)
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.ContentLength = int64(len(body))
		return resp, nil
	}
}
