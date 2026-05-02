# Development Notes

Clonarr is Docker-first. The production path builds the Go binary, embeds the static UI, and runs it in the Alpine image with `/config` mounted for persistent state.

## Docker Build From Source

```bash
docker build -t clonarr .
docker run -d --name clonarr -p 6060:6060 \
  -v ./config:/config clonarr
```

The local `./config` directory is ignored by git because Docker source-build testing commonly creates it. If the browser shows behavior that does not match the current container, confirm what owns the port:

```bash
ss -ltnp 'sport = :6060'
```

A listener under `/tmp/go-build.../exe/clonarr` is a leftover `go run` process, not Docker.

## UI Structure

The frontend is a single Alpine.js app, but the source is split by responsibility:

- `ui/static/index.html` is the root Go template. It composes layout, section, overlay, and modal partials from `ui/static/partials/**`.
- `ui/static/js/main.js` registers the Alpine `clonarr` factory and combines `state.js` with feature modules from `ui/static/js/features/**`.
- Feature modules export `{ state, methods }`. `main.js` merges all state first, then methods, so getters and methods can reference state from any feature.
- `ui/static/css/styles.css` is the CSS entrypoint. It imports design tokens, base/layout/component rules, and feature CSS files.
- Alpine is self-hosted at `ui/static/js/vendor/alpine.min.js`; keep `main.js` before Alpine in `index.html` so the `alpine:init` registration happens before Alpine starts.

`main.go` parses `index.html` plus the partial glob patterns at startup. A missing or renamed partial fails fast during startup instead of producing a half-rendered UI.

## UI Manifest

`GET /api/ui/manifest` is the shared metadata layer between Go and Alpine. It exposes:

- option lists for dropdowns such as sync behavior, auth mode, app type, and pull interval;
- numeric bounds such as session TTL;
- category and profile-group color metadata;
- notification provider labels and form field specs.

When adding a new enum value, category, profile group, or notification provider, prefer updating the Go source of truth in `internal/core` or `internal/core/agents` and let the manifest drive the UI. Add hard-coded frontend options only when the value is truly client-only.

## Auth And Config Directory

`CONFIG_DIR` defaults to `/config` in Docker. The auth store receives that same directory so `auth.json` and `sessions.json` live beside `clonarr.json`, profiles, sync history, custom CFs, and TRaSH cache data. This keeps alternate deployments and tests from accidentally writing auth files to a different root than the rest of the application.
