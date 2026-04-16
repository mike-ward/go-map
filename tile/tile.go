// Package tile provides tile coordinates, tile sources, and a small
// LRU cache for slippy-tile map widgets.
package tile

import (
	"context"
	"errors"
	"fmt"
)

// Coord is a slippy-tile address: zoom, column, row.
// Row 0 is the northernmost row at the given zoom.
type Coord struct {
	Z uint32
	X uint32
	Y uint32
}

// String returns the canonical {z}/{x}/{y} form used in most tile URLs.
func (c Coord) String() string {
	return fmt.Sprintf("%d/%d/%d", c.Z, c.X, c.Y)
}

// Valid reports whether c is in range for its zoom level.
func (c Coord) Valid() bool {
	n := uint32(1) << c.Z
	return c.X < n && c.Y < n
}

// ErrNotFound is returned by a Source when a tile does not exist.
var ErrNotFound = errors.New("tile: not found")

// Source supplies encoded tile image bytes for a Coord.
// Implementations must be safe for concurrent use.
type Source interface {
	// Fetch returns encoded image bytes (PNG/JPEG/WebP). The caller
	// owns the returned slice.
	Fetch(ctx context.Context, c Coord) ([]byte, error)

	// URL returns a string the rendering layer can pass to
	// gui.DrawContext.Image. For HTTP sources this is the tile URL;
	// for future offline sources it may be a data: URL or a local
	// path. Empty string means the Source cannot render through the
	// URL path and the caller must use Fetch instead.
	URL(c Coord) string

	// Attribution returns a short, human-readable credit string to be
	// rendered by the map widget. Required by most tile providers.
	Attribution() string

	// MaxZoom returns the highest zoom level this source serves.
	MaxZoom() uint32
}
