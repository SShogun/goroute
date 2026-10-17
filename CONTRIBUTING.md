# Contributing to GoRoute

Thank you for your interest in contributing to GoRoute!

## Philosophy
1. **Ultra-Resource Efficiency First**: Every change must respect the target of < 15MB RAM idle. Do not introduce heavy background runtimes, persistent open sockets, or unneeded dependencies.
2. **Standard Library Over Frameworks**: Favor Go's standard library and lightweight packages (`chi`, `cobra`).
3. **Evidence Before Assertions**: All PRs must include passing unit tests and benchmark checks.

## Development Setup

```bash
git clone https://github.com/ziuus/goroute.git
cd goroute
make test
make build
```

## Adding a New Provider Adapter

1. Create a package in `pkg/providers/<name>/<name>.go`.
2. Implement the `providers.Provider` interface:
   - `ID() string`
   - `Type() string`
   - `ChatCompletion(ctx, req) (body, isStream, statusCode, err)`
   - `Models(ctx) ([]ModelInfo, error)`
   - `IsHealthy() bool`
3. Add a unit test with mock HTTP server in `pkg/providers/<name>/<name>_test.go`.
4. Register the new provider type in `cmd/goroute/commands.go`.

## Pull Request Guidelines
- Branch from `main`.
- Ensure `make test` and `make bench` pass cleanly.
- Commit messages should follow Conventional Commits (e.g. `feat:`, `fix:`, `docs:`).
