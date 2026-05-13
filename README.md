<p align="center">
  <img src="src/img/logo.svg" alt="SLayer" width="120">
</p>

<h1 align="center">SLayer for Grafana</h1>

<p align="center">
  <strong>Grafana data source plugin for <a href="https://github.com/MotleyAI/slayer">SLayer</a></strong> — Motley's open-source, agent-first semantic layer.
</p>

<p align="center">
  Build dashboards over the same structured query DSL your AI agents use over MCP, against any of <a href="https://motley-slayer.readthedocs.io/en/latest/configuration/datasources/#supported-database-types">SLayer's supported databases</a> — Postgres, MySQL, ClickHouse, Snowflake, BigQuery, DuckDB, SQLite, and more.
</p>

> **Status: beta.** PRs and issues welcome.

<p align="center">
  <img src="docs/images/dashboard-overview.jpg" alt="SLayer for Grafana — demo dashboard on Jaffle Shop data, with cohort retention, store leaderboard, daily revenue and KPIs" width="900">
</p>

## Try it in 30 seconds

The only thing you need on your machine is **Docker Compose**:

```bash
git clone https://github.com/MotleyAI/grafana-slayer-datasource
cd grafana-slayer-datasource
docker compose up
```

Open <http://localhost:3000/d/slayer-jaffle-demo/>: **Dashboards → SLayer → Jaffle Shop**. Everything builds inside Docker — no Node, Go, or Python on your host. First run takes ~5 minutes (frontend + backend + Grafana, hermetic multi-stage build); subsequent boots take seconds. A local SLayer instance with the bundled Jaffle Shop demo data on DuckDB comes up alongside Grafana.

## What is SLayer?

SLayer sits between your database and your AI agents (and dashboards, scripts, internal tools). Instead of writing raw SQL, you describe *what* you want — measures, dimensions, filters — and SLayer compiles it to the right SQL dialect, handling joins, time arithmetic, and per-engine quirks.

A query like

```json
{
  "source_model": "orders",
  "measures": ["cumsum(revenue:sum)", "change_pct(revenue:sum)"],
  "time_dimensions": [{"dimension": "ordered_at", "granularity": "month"}]
}
```

produces *"month-on-month % change in cumulative revenue"* — no one writes window functions. SLayer's DSL [supports](https://motley-slayer.readthedocs.io/en/latest/concepts/queries/) measure formulas, time shifts, joined dimensions, multi-stage queries, queries-as-models, and a lot more.

### What SLayer brings to the table

- **Auto-ingestion of your schema** — introspects tables and foreign keys, generates models with explicit join metadata.
- **Query-time aggregations** — pick `:sum` / `:avg` / `:count_distinct` per query, not at model definition time.
- **Composable transforms** — `cumsum`, `time_shift`, `change_pct`, `lag`, `lead`, `rank`, `percentile`, more.
- **Dialect-aware compilation** — same query, correct SQL for every backend.
- **Saved memories** — agents and humans can record natural-language notes tied to specific entities; SLayer surfaces them on future queries.
- **Schema-drift detection** — when the live database diverges from a saved model, SLayer flags it instead of generating broken SQL.

### Three surfaces, one DSL

| Consumer | Surface | What it does |
|---|---|---|
| **Humans** | This Grafana plugin | Builds dashboards on SLayer models — query editor with form + JSON modes |
| **AI agents** 🤖 | [SLayer's MCP server](https://github.com/MotleyAI/slayer?tab=readme-ov-file#mcp-server) | Tools to introspect models, run queries, save memories, ingest new datasources |
| **Code** | [REST](https://github.com/MotleyAI/slayer?tab=readme-ov-file#rest-api), [Python](https://github.com/MotleyAI/slayer?tab=readme-ov-file#python-client), [CLI](https://github.com/MotleyAI/slayer?tab=readme-ov-file#cli) | Same models, same DSL, embeddable as a Python library or run as a server |

That's the central thesis: model your data once, query it from anywhere using the same vocabulary. No more "SQL written by humans" vs "SQL written by agents" divergence.

## Attach your AI agent

SLayer ships an MCP server (HTTP/SSE and stdio) exposed alongside its REST API. Point Claude Code at the running demo:

```bash
claude mcp add slayer --transport sse --url http://localhost:5143/mcp/sse
```

Now your agent has tools to introspect models, run queries, save and recall memories, and create new datasources — all on the same data your Grafana dashboard sees. See [SLayer's MCP docs](https://motley-slayer.readthedocs.io/en/latest/reference/mcp/).

> 🗺️ **Roadmap: plugin-hosted MCP for dashboard authoring.** A future MCP surface will let agents *create Grafana panels and dashboards* from natural-language prompts — e.g. *"show me revenue per store as a bar chart over the last quarter."* Open an issue if you want to help shape it.

## How the plugin works

The Go backend is a thin proxy: it forwards your panel's query payload to SLayer's `POST /query`, converts the response into a Grafana data frame, sends it back. Your data never lands in the plugin's storage — SLayer talks to your DB directly.

A single Grafana data source instance points at a single SLayer instance; SLayer's internal datasource selection (which Postgres? which ClickHouse?) is per-query, exposed in the query editor.

Three quality-of-life features the plugin adds on top of the raw REST call:

- **Time-range auto-injection**. Grafana's dashboard time range is auto-populated as `{__from}`, `{__to}`, `{__from_ms}`, `{__to_ms}`, `{__interval_ms}` variables on every query — you can reference them directly in filters (`ordered_at >= '{__from}'`). If your query declares a `time_dimension` and no filter mentions the macros, a default time filter is auto-added.
- **Template variables**. Dashboard dropdown variables can be populated from a SlayerQuery: write the JSON in the variable definition (e.g. `{"source_model":"orders","dimensions":[{"name":"store_id"}]}`) and the plugin's `metricFindQuery` projects the first column into dropdown options.
- **Form + JSON query editor**. Common queries are built with a form (model, measures, dimensions, time dim + granularity, filters); the "JSON" toggle lets power users drop into the raw `SlayerQuery` for the full DSL.

<p align="center">
  <img src="docs/images/query-editor.jpg" alt="SLayer query editor in a Grafana panel — model, measures, dimensions, time dimension + granularity, filters, limit" width="800">
</p>

### Multi-stage queries, one panel

Cohort analysis, period-over-period comparisons, queries-as-models — anything that needs an intermediate aggregation to feed a final one — is a first-class shape in the SLayer DSL. The plugin's editor exposes the full DAG inline: a form for the outer query plus a collapsible list of named sub-queries, each editable with the same controls (no JSON wrangling required).

The cohort retention table in the demo dashboard is built this way — one sub-query derives each customer's first-order month, another joins orders back and computes the month-since-cohort offset, the outer query counts active customers per `(cohort, period)`. Three composable SlayerQuery stages, one panel, the same DSL your AI agents use over MCP.

<p align="center">
  <img src="docs/images/cohort-editor.jpg" alt="Multi-stage query editor showing the cohort retention setup — outer aggregate plus two named sub-queries (customer_first, enriched) editable inline" width="800">
</p>

## Use it with your own database

1. **Run SLayer** pointed at your database — `pip install motley-slayer && slayer serve`, then [add a data source](https://github.com/MotleyAI/slayer?tab=readme-ov-file#datasource-setup) and [ingest models](https://github.com/MotleyAI/slayer?tab=readme-ov-file#auto-ingestion). Or use the MCP tools to have your agent do it for you.
2. **Add a SLayer data source** in Grafana — paste the URL (e.g. `http://localhost:5143`). The "Save & test" button calls SLayer's `/health` to verify connectivity.
3. **Build dashboards.** Model names autocomplete from `GET /models`; the query editor handles measures, dimensions, time dimensions with granularity, and filters; advanced users drop into JSON for the full DSL.

## Development & contributing

Tooling, build, test, dev-container, roadmap — see [CONTRIBUTING.md](./CONTRIBUTING.md).

## License & links

MIT. Built for [SLayer](https://github.com/MotleyAI/slayer) by [Motley](https://motley.ai), runs on [Grafana](https://grafana.com).
