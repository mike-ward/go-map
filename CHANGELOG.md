# Changelog

All notable changes are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## Unreleased

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
