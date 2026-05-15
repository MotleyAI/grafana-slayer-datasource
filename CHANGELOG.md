# Changelog

## 0.1.0

First public beta of the SLayer data source plugin for Grafana.

### Datasource

- Go backend proxies queries to SLayer's REST API (`POST /query`).
- Health check round-trips through SLayer's `/health`.
- Forward-compat API-key support in `secureJsonData.apiKey` (sent as `Authorization: Bearer …` when set; SLayer ≤0.6.x ignores it).

### Query editor

- **Form mode** — model name, measures (one per line with optional `formula AS name` syntax), dimensions, time dimensions + global granularity dropdown (`none` / `second` / … / `year`), filters, row limit.
- **JSON mode** — raw `SlayerQuery` payload for the full DSL (transforms, nested via `name` run-by-name, etc.).
- Toggle between modes preserves state.

### Auto-injected time variables

- Every query gets `{__from}`, `{__to}`, `{__from_ms}`, `{__to_ms}`, `{__interval_ms}` populated from the dashboard time range. Reference them directly in filter strings.
- When a query declares a `time_dimension` and no filter references the macros, a default `<time_dim> >= '{__from}' AND <time_dim> <= '{__to}'` is auto-injected.
- Frame fields for time dimensions are emitted as `time.Time`, so time-series panels render the axis correctly.

### Template variables (`metricFindQuery`)

- Dashboard dropdown variables can be populated from a SlayerQuery — write the JSON in the variable definition; the plugin's `/resources/metric-find` runs it and projects the first column's distinct values to `MetricFindValue[]`.

### Demo dashboard

- `docker compose up` (Docker-only — multi-stage `Dockerfile` builds the plugin inside the image) brings up Grafana 12.4 with the plugin pre-installed and a polished Jaffle Shop dashboard auto-provisioned: total orders, total revenue, average order value, unique customers, daily revenue, revenue by store, cohort retention table, and store leaderboard.

### Column order

- The Go backend emits frame fields in **query spec order** (dimensions → time_dimensions → measures, each list in user order; unrecognized columns trail). SLayer's response columns are alphabetical otherwise, so this surfacing matches user intent.

### Format hint passthrough

- SLayer's per-column `attributes.measures.<col>.format` (`{type, precision, symbol}`) now translates automatically into Grafana's `FieldConfig.Unit` + `FieldConfig.Decimals` on the returned frame. Mapping: `integer`/`float` → `short`, `percent` → `percent`, `currency` + symbol → `currencyUSD`/`EUR`/`GBP`/`JPY`/`RUB`/`INR`/`CHF` (unknowns fall back to `currencyUSD`). Panel-side `fieldConfig.defaults.unit` still overrides — Grafana's merge order is frame < panel defaults < overrides, so a dashboard that wants a specific unit can specify one and win.

### Alerting

- `plugin.json` declares `alerting: true`. SLayer-backed queries can now be the source of Grafana alert rules — the alert evaluator calls the same `QueryData` handler that powers panels, and our auto-injected `{__from}`/`{__to}` variables work the same way under the alerting scheduler.

### Plugin-hosted MCP server

- The plugin serves its own MCP **Streamable HTTP** transport through Grafana's `CallResource` path:
  ```
  http://<grafana>/api/datasources/uid/<slayer-ds>/resources/mcp
  ```
  Three tools: `list_dashboards`, `inspect_dashboard`, `add_panel_to_dashboard`. Each accepts a SlayerQuery (the same shape SLayer's MCP `query` tool produces), so the natural loop is: agent queries SLayer to iterate on the right query, then calls `add_panel_to_dashboard` to land it on a real dashboard. Grafana's own auth (session, service-account token, SSO) gates incoming MCP requests; the plugin uses a service-account token configured on the SLayer datasource (`secureJsonData.grafanaToken`) for outbound write calls. The bundled demo works without one because anonymous-Admin auth is enabled.
- New package layout: `pkg/grafana` (REST client) + `pkg/mcp` (tool registration + panel construction), wired into the plugin's `CallResource` handler.

### Compatibility

- Grafana 11.0+ (`grafanaDependency: ">=11.0.0"`).
- Linux amd64 + arm64 binaries shipped in the plugin bundle (Apple Silicon hosts under Docker Desktop and standard x86 servers both work).
