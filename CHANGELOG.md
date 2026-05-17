# Changelog

All notable changes are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## Unreleased

## v0.4.1 — 2026-05-17

### Changed
- Bump `go-gui` to v0.19.1 (scroll phase bridge, context menu focus fix,
  animation heartbeat, Metal autorelease fix)
- Bump `go-glyph` to v1.7.1 (indirect)

## v0.4.0 — 2026-04-30

### Changed
- Bump `go-gui` to v0.17.0
- Bump `go-glyph` to v1.7.0 (indirect)

## v0.3.2 — 2026-04-26

### Changed
- `mapview`: split `input.go` into `keyboard.go`, `pan.go`, `scroll.go`;
  harden non-finite coordinates
- Bump `go-gui` to v0.12.7

### Added
- Docs: `tile-mapview` deep-dive; ignore antivibe deep-dive dir

## v0.3.1 — 2026-04-19

### Added
- Architecture document (`docs/architecture.md`)
- Benchmark test suites for `tile`, `tile/wms`, and `mapview`

### Changed
- `mapview`: state-version counter (`bumpVersion`) replaces per-frame
  `FrameCount` version — no-op frames replay cached tessellation without
  calling `OnDraw`
- `mapview`: fractional-zoom scale-bar spacing tolerance relaxed to 0.01 px

### Fixed
- Various lint and code-review fixes across `tile` and `mapview`

## v0.1.0 — initial

### Added
- `projection`: `LatLng`, `Point`, `Bounds`, Web Mercator
  `Project`/`ProjectF`/`Unproject`/`UnprojectF`; `TileSize = 256`
- `tile`: `Coord{Z,X,Y}`, `Source` interface, `OSM()` / `OSMWithUserAgent()`
  adapters, LRU `Cache`
- `mapview`: interactive tile rendering, pan/zoom state registry, input
  handlers, scale bar, attribution, home button, `OnMove`/`OnZoomChange`
  callbacks
- Phase 1 tests: viewport math, zoom-to-cursor, dateline wrap, OSM UA
- `examples/basic`: minimal runnable OSM demo
