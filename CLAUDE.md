## Project knowledge

This repository contains a **Grafana plugin**. You must Read @./.config/AGENTS/instructions.md before doing changes.

## SLayer-Grafana plugin specifics

This plugin is a Grafana **data source** for [SLayer](https://github.com/MotleyAI/slayer), Motley's semantic layer. The Go backend proxies queries to SLayer's REST API (`POST /query` on port 5143). SLayer's sibling repo lives at `../slayer` on disk.

### Local dev requirements

- **`GOTOOLCHAIN=auto`** must be set when running Go commands — the scaffolded `go.mod` requires Go ≥ 1.25 and Go will auto-download the right toolchain when this is set. Either `go env -w GOTOOLCHAIN=auto` once, or use the `pnpm check:backend` / `pnpm check` scripts (they set it inline).
- **Mage** (`go install github.com/magefile/mage@latest`) — required for backend builds. The binary lives in `$(go env GOPATH)/bin`; add that to your PATH.

### Verification target

`pnpm check` runs typecheck + lint + jest + `go vet` + `go test` end-to-end. Use this as the primary "did I break anything" feedback loop. It does **not** run the production webpack build or cross-compile the Go backend — invoke `pnpm build` and `mage build:backend` explicitly when those matter.

### Two docker-compose configurations

- **`docker-compose.yaml`** (root, default) — end-user demo. Uses the root `Dockerfile` (multi-stage: builds the frontend with Node, cross-compiles the Go backend, drops both into stock `grafana/grafana-enterprise:12.4.0`). End users need only Docker. Auto-provisions the datasource + Jaffle Shop demo dashboard. Run via `docker compose up` (use `--build` to force a rebuild after source changes).
- **`docker-compose.dev.yaml`** — plugin development. Uses `.config/Dockerfile` to build a dev image with delve + supervisord + `mage watch` for in-container hot-reload of Go/TS changes. Source is bind-mounted instead of baked. Run via `pnpm server`.

**Backend changes still need `docker compose -f docker-compose.dev.yaml restart grafana` to reload** even in the dev container — the build-watcher only reattaches delve, not Grafana. (See the dev-container plugin reload memory note.)

### Docker compose conventions

- Always launch with `-d` (detached): `docker compose up --build -d`. The user runs Claude Code inside Zed; foreground `docker compose up` hangs the integration.
- After `up -d`, poll readiness with `curl -fsS http://localhost:3000/api/health` in a separate command before hitting any datasource APIs.
- For logs, use `docker compose logs grafana --tail 50` or `docker logs grafana` rather than tailing the foreground process.

### BuildKit cache mis-invalidation (gotcha)

`docker compose up --build` does **not** always pick up Go source changes. BuildKit may decide a layer's content hash matches a previous build and re-use the cached binary, leaving the new code on disk but the old binary in the image.

**Symptom:** you edited `pkg/...`, rebuilt, but the running plugin still behaves like the old version.

**Quickest tell:** the binary's mtime inside the container:

```bash
docker exec grafana ls -la /var/lib/grafana/plugins/motley-slayer-datasource/ | grep gpx_slayer
```

If it's older than your `pkg/` edits, the cache fooled `--build`.

**Reliable fix:**

```bash
docker compose down
docker compose build --no-cache
docker compose up -d
```

Slower (~5 min cold) but guaranteed to rebuild from source. Sanity-check via `/api/ds/query` (cheaper than re-rendering the dashboard, and bypasses Grafana's frontend caching).
