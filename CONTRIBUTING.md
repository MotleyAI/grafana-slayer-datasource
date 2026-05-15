# Contributing

The plugin is in beta. We welcome bug reports and PRs — see the [roadmap](#roadmap) for known gaps and likely directions.

## Toolchain

For active plugin development (anything beyond the Docker-based end-user demo) you'll need:

| Tool | Version | Notes |
|---|---|---|
| Node.js | 22+ | Frontend (TS/React) |
| pnpm | 10+ | `corepack enable` picks the version pinned in `package.json` |
| Go | 1.25+ | Or any newer Go with `GOTOOLCHAIN=auto` set so it auto-downloads the right toolchain |
| [Mage](https://magefile.org/) | latest | `go install github.com/magefile/mage@latest`; lives in `$(go env GOPATH)/bin` |
| Docker | Compose v2 | For the dev container and end-user demo |

> 📦 Only **Docker** is needed for the end-user demo (`docker compose up`). Everything below is for hacking on the plugin itself.

## Setup

```bash
pnpm install
```

## Primary feedback loop

`pnpm check` runs the full local verification suite end-to-end:

- `tsc --noEmit` (frontend typecheck)
- `eslint` (frontend lint)
- `jest` (frontend unit tests)
- `go vet` (backend static analysis)
- `go test` (backend unit tests)

Treat it as the *"did I break anything?"* command. It does **not** run the production webpack build or cross-compile the Go backend — invoke those separately when relevant (`pnpm build`, `mage build:backend` / `mage build:linux`).

## Building

| Target | Command |
|---|---|
| Frontend (dev, watch) | `pnpm dev` |
| Frontend (prod, one-shot) | `pnpm build` |
| Backend plugin (current platform) | `mage build:backend` |
| Backend plugin (cross-compile for Docker) | `mage build:linux build:linuxARM64` |

## Running

### End-user demo (Docker-only)

```bash
docker compose up
```

Builds the multi-stage image defined in the root `Dockerfile` (frontend + backend + Grafana) and runs it alongside a SLayer instance with the Jaffle Shop demo. Open <http://localhost:3000>.

Add `--build` to force a rebuild after code changes.

### Plugin development (hot-reload)

```bash
pnpm server
```

Wraps `docker compose -f docker-compose.dev.yaml up --build`. Builds the dev image (`./.config/Dockerfile`) which carries delve, supervisord, and `mage watch` + `webpack --watch` running inside. Source is bind-mounted, so edits in `src/` and `pkg/` trigger rebuilds.

> ⚠️ **Backend rebuilds inside the dev container don't auto-reload the running plugin.** Grafana owns the plugin subprocess lifecycle (HashiCorp `go-plugin`), so a fresh binary on disk has no effect until Grafana respawns it. After a Go change, run:
>
> ```bash
> docker compose -f docker-compose.dev.yaml restart grafana
> ```
>
> Frontend changes are picked up live by webpack's HMR and don't need a restart.

## Architecture

```
┌───────────────────────┐  HTTP+gRPC subprocess (UDS)   ┌──────────────────────┐
│  Grafana (web + API)  │ ────────────────────────────► │  Our Go plugin       │
│  ./provisioning baked │                               │  pkg/plugin          │
└───────────────────────┘                               │  pkg/slayer (client) │
        ▲                                               └────────┬─────────────┘
        │                                                        │ HTTP / JSON
        │                                                        ▼
        │                                               ┌──────────────────────┐
   browser/UI                                           │   SLayer server      │
                                                        │   :5143 REST + MCP   │
                                                        └────────┬─────────────┘
                                                                 │ dialect-aware SQL
                                                                 ▼
                                                        ┌──────────────────────┐
                                                        │   your database      │
                                                        └──────────────────────┘
```

### Layout

```
├── src/                     # TS/React frontend
│   ├── components/          # ConfigEditor.tsx, QueryEditor.tsx
│   ├── datasource.ts        # DataSourceWithBackend subclass + metricFindQuery
│   ├── module.ts            # Plugin entry — wires DS + Config + Query editors
│   ├── plugin.json          # Plugin manifest
│   └── types.ts             # SlayerQuery, SlayerOptions, ...
├── pkg/                     # Go backend (the Grafana plugin subprocess)
│   ├── plugin/              # QueryData, CheckHealth, CallResource handlers
│   ├── slayer/              # HTTP client for SLayer REST
│   ├── grafana/             # HTTP client for Grafana REST (used by MCP tools)
│   ├── mcp/                 # MCP tool registration + panel construction
│   └── models/              # Plugin settings
├── provisioning/            # Auto-loaded datasource + demo dashboard
├── .config/                 # Grafana plugin tools build infra (do not modify)
├── Dockerfile               # End-user demo image (multi-stage)
├── docker-compose.yaml      # End-user demo compose
└── docker-compose.dev.yaml  # Plugin development compose (hot-reload)
```

`.config/` is managed by `@grafana/create-plugin` — don't modify by hand. Running `npx @grafana/create-plugin update` refreshes it.

## Testing

| Suite | Command | Notes |
|---|---|---|
| Frontend unit (Jest) | `pnpm test` (watch) or `pnpm test:ci` | No frontend tests yet — open a PR if you add some |
| Backend unit (Go) | `go test ./pkg/...` | Solid coverage; new behaviour should ship with tests |
| E2E (Playwright) | `pnpm e2e` | Wired but not enabled by default — opt in by adding `playwright.config.ts` and a `tests/` directory back |

## Troubleshooting

### `docker compose up --build` doesn't pick up my Go changes

BuildKit's content-hash cache can occasionally decide a `COPY pkg ./pkg` layer matches a previous build and re-use the cached binary — so the new code is on disk but the old binary is in the image.

**Quickest tell:** compare the binary mtime inside the container to your source edits:

```bash
docker exec grafana ls -la /var/lib/grafana/plugins/motley-slayer-datasource/ | grep gpx_slayer
```

If it predates your `pkg/` changes, BuildKit fooled `--build`.

**Reliable fix:**

```bash
docker compose down
docker compose build --no-cache
docker compose up -d
```

Slower (~5 min cold) but always picks up source changes. To confirm without a browser, hit `/api/ds/query` and inspect the frame's `schema.fields` directly — bypasses Grafana's frontend cache.

### Dashboard still shows old data after rebuild

Grafana's frontend caches the last frame per panel. After a backend rebuild + container restart, hard-refresh the dashboard tab (Cmd-Shift-R on macOS, Ctrl-Shift-R elsewhere) to force the panel to re-query.

### Backend changes inside the dev container don't take effect

Grafana owns the plugin subprocess lifecycle. The in-container `mage watch` rebuilds the binary on Go edits, but Grafana keeps running the old subprocess until you bounce it:

```bash
docker compose -f docker-compose.dev.yaml restart grafana
```

Frontend changes don't need a restart — webpack HMR handles them live.

## Plugin signing / publication

To run unsigned on a vanilla Grafana install, set `GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=motley-slayer-datasource` (our compose files already do this). For a publicly-installable release through the Grafana plugin catalog, follow [Grafana's plugin signing guide](https://grafana.com/developers/plugin-tools/publish-a-plugin/sign-a-plugin) — `.github/workflows/release.yml` is wired for it; uncomment the `policy_token` block and add the `GRAFANA_ACCESS_POLICY_TOKEN` secret to the repo.

## Roadmap

Known gaps and likely follow-ups, roughly ordered:

- **Time-series multi-series pivot** — `toFrame` currently returns table-shaped frames; multi-series time charts work but stack series awkwardly. Detect `1 time_dim + N other dims + measure` → wide-pivot frame.
- **Model-name autocomplete in QueryEditor** — backend `/resources/models` exists; the editor still uses a free-text model input.
- **Frontend unit tests** — zero coverage on `ConfigEditor` / `QueryEditor`.
- **Grafana `Combobox` migration** — the deprecated `Select` component still works, but a future Grafana release will remove it.
- **More MCP authoring tools** — the current set (`list_dashboards`, `inspect_dashboard`, `add_panel_to_dashboard`) covers the common loop; deeper operations (panel edit/delete, layout, alert rule creation) are next.

## Contributing

1. Open an issue describing the change or bug; we'll discuss approach.
2. Fork → branch → PR. Make sure `pnpm check` passes locally.
3. New behavior should ship with tests — backend tests are easy; frontend tests don't exist yet but we welcome the first.

## License

MIT — see [LICENSE](./LICENSE).
