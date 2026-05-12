## Project knowledge

This repository contains a **Grafana plugin**. You must Read @./.config/AGENTS/instructions.md before doing changes.

## SLayer-Grafana plugin specifics

This plugin is a Grafana **data source** for [SLayer](https://github.com/MotleyAI/slayer), Motley's semantic layer. The Go backend proxies queries to SLayer's REST API (`POST /query` on port 5143). SLayer's sibling repo lives at `../slayer` on disk.

### Local dev requirements

- **`GOTOOLCHAIN=auto`** must be set when running Go commands — the scaffolded `go.mod` requires Go ≥ 1.25 and Go will auto-download the right toolchain when this is set. Either `go env -w GOTOOLCHAIN=auto` once, or use the `pnpm check:backend` / `pnpm check` scripts (they set it inline).
- **Mage** (`go install github.com/magefile/mage@latest`) — required for backend builds. The binary lives in `$(go env GOPATH)/bin`; add that to your PATH.

### Verification target

`pnpm check` runs typecheck + lint + jest + `go vet` + `go test` end-to-end. Use this as the primary "did I break anything" feedback loop. It does **not** run the production webpack build or cross-compile the Go backend — invoke `pnpm build` and `mage build:backend` explicitly when those matter.

### License

MIT (matches SLayer). The scaffolded `LICENSE` was Apache-2.0 and was replaced; if you regenerate from `@grafana/create-plugin`, restore MIT.
