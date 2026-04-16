# Contributing

## Prerequisites

- Go 1.26+
- SDL2 + glyph dependencies (see go-gui README)

## Build and Test

```
go build ./...
go vet ./...
go test ./...
golangci-lint run ./...
gofmt -l .
```

All must pass before committing.

## Coding Conventions

- No variable shadowing. Use `=` to reassign, not `:=`.
- Widgets follow the `*Cfg` struct pattern from go-gui.
- Event callbacks: `func(*gui.Layout, *gui.Event, *gui.Window)`.
- Wrap comments at 90 columns when practical.
- Favor reducing heap allocations.
- Use glyph (via go-gui) for text; consult it before writing new text routines.

## License

Contributions accepted under [MIT](LICENSE).
