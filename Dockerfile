# syntax=docker/dockerfile:1.7
# Build image for the slayer-grafana plugin baked into Grafana. End users need
# only Docker — `docker compose up` builds and runs everything.
# Three stages:
#   1. frontend   — webpack TS/React → dist/module.js + plugin.json + assets
#   2. backend    — mage cross-compiles gpx_slayer_linux_{amd64,arm64} binaries
#   3. runtime    — stock Grafana + the plugin dropped into /var/lib/grafana/plugins/

# ──────────────────────────────────────────────────────────────────────────
# Stage 1: frontend
# ──────────────────────────────────────────────────────────────────────────
FROM node:22-bookworm-slim AS frontend
WORKDIR /build

# Enable corepack and pin the pnpm version used by package.json.
RUN corepack enable && corepack prepare pnpm@10.14.0 --activate

COPY package.json pnpm-lock.yaml .npmrc ./
RUN --mount=type=cache,id=pnpm-store,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile

COPY tsconfig.json eslint.config.mjs .prettierrc.js ./
COPY .config ./.config
COPY src ./src
# webpack's CopyWebpackPlugin folds these into dist/
COPY LICENSE README.md CHANGELOG.md ./

RUN pnpm build

# ──────────────────────────────────────────────────────────────────────────
# Stage 2: backend
# ──────────────────────────────────────────────────────────────────────────
FROM golang:1.25-bookworm AS backend
WORKDIR /build
ENV GOTOOLCHAIN=auto

RUN go install github.com/magefile/mage@latest

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY Magefile.go ./
# plugin.json carries the plugin ID + version that mage embeds into the
# binary's ldflags; the SDK build helper reads it from src/plugin.json.
COPY src/plugin.json ./src/plugin.json
COPY pkg ./pkg

# Cross-compile both Linux architectures so the runtime image works on Intel
# and ARM hosts (Apple Silicon under Docker Desktop runs linux/arm64).
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    mage build:linux build:linuxARM64

# ──────────────────────────────────────────────────────────────────────────
# Stage 3: runtime — Grafana + plugin baked in
# ──────────────────────────────────────────────────────────────────────────
FROM grafana/grafana-enterprise:12.4.0

# Allow this unsigned plugin to load. (docker-compose can override at runtime
# too; baking the default here means the image works standalone.)
ENV GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=motley-slayer-datasource

# Frontend artifacts (module.js, plugin.json, img/, copied LICENSE/README/CHANGELOG)
COPY --from=frontend /build/dist /var/lib/grafana/plugins/motley-slayer-datasource

# Backend binaries (both archs — Grafana picks the matching one at spawn time)
COPY --from=backend /build/dist/gpx_slayer_linux_amd64 /var/lib/grafana/plugins/motley-slayer-datasource/
COPY --from=backend /build/dist/gpx_slayer_linux_arm64 /var/lib/grafana/plugins/motley-slayer-datasource/
COPY --from=backend /build/dist/go_plugin_build_manifest /var/lib/grafana/plugins/motley-slayer-datasource/
