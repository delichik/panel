# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM node:22-alpine AS web-build
WORKDIR /src/web

COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
  npm config set fetch-retries 5 \
  && npm config set fetch-retry-factor 2 \
  && npm config set fetch-retry-mintimeout 10000 \
  && npm config set fetch-retry-maxtimeout 120000 \
  && npm config set fetch-timeout 300000 \
  && npm ci

COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS go-source
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
  go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/

RUN --mount=type=cache,target=/go/pkg/mod \
  --mount=type=cache,target=/root/.cache/go-build \
  go run ./cmd/generate-agent-contract-hash

FROM go-source AS agent-bundle-build
ARG PANEL_VERSION=dev
ARG PANEL_CHANNEL=dev
ARG PANEL_REPOSITORY
ARG PANEL_COMMIT
RUN --mount=type=cache,target=/go/pkg/mod \
  --mount=type=cache,target=/root/.cache/go-build \
  set -eux; \
  verify_machine() { \
    binary="$1"; \
    arch="$2"; \
    machine="$(od -An -tx1 -j18 -N2 "${binary}" | tr -d ' \n')"; \
    case "${arch}/${machine}" in \
      amd64/3e00|arm64/b700) ;; \
      *) echo "compiled ${binary} ELF machine ${machine} does not match GOARCH=${arch}"; exit 1 ;; \
    esac; \
  }; \
  ldflags="-s -w -X panel/internal/platform/buildinfo.Version=${PANEL_VERSION} -X panel/internal/platform/buildinfo.Channel=${PANEL_CHANNEL} -X panel/internal/platform/buildinfo.Repository=${PANEL_REPOSITORY} -X panel/internal/platform/buildinfo.Commit=${PANEL_COMMIT}"; \
  mkdir -p /out/panel-agents; \
  for platform in linux/amd64 linux/arm64; do \
    agent_os="${platform%/*}"; \
    agent_arch="${platform#*/}"; \
    agent_dir="/out/panel-agents/${agent_os}-${agent_arch}"; \
    mkdir -p "${agent_dir}"; \
    CGO_ENABLED=0 GOOS="${agent_os}" GOARCH="${agent_arch}" go build -trimpath \
      -ldflags="${ldflags}" \
      -o "${agent_dir}/panel-agent" ./cmd/panel-agent; \
    verify_machine "${agent_dir}/panel-agent" "${agent_arch}"; \
  done

FROM go-source AS target-binaries-build
ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH
ARG PANEL_VERSION=dev
ARG PANEL_CHANNEL=dev
ARG PANEL_REPOSITORY
ARG PANEL_COMMIT
RUN --mount=type=cache,target=/go/pkg/mod \
  --mount=type=cache,target=/root/.cache/go-build \
  set -eux; \
  test -n "${TARGETPLATFORM}"; \
  test -n "${TARGETOS}"; \
  test -n "${TARGETARCH}"; \
  case "${TARGETPLATFORM}" in \
    "${TARGETOS}/${TARGETARCH}"|"${TARGETOS}/${TARGETARCH}/"*) ;; \
    *) echo "TARGETPLATFORM=${TARGETPLATFORM} does not match ${TARGETOS}/${TARGETARCH}"; exit 1 ;; \
  esac; \
  verify_machine() { \
    binary="$1"; \
    arch="$2"; \
    machine="$(od -An -tx1 -j18 -N2 "${binary}" | tr -d ' \n')"; \
    case "${arch}/${machine}" in \
      amd64/3e00|arm64/b700) ;; \
      *) echo "compiled ${binary} ELF machine ${machine} does not match GOARCH=${arch}"; exit 1 ;; \
    esac; \
  }; \
  ldflags="-s -w -X panel/internal/platform/buildinfo.Version=${PANEL_VERSION} -X panel/internal/platform/buildinfo.Channel=${PANEL_CHANNEL} -X panel/internal/platform/buildinfo.Repository=${PANEL_REPOSITORY} -X panel/internal/platform/buildinfo.Commit=${PANEL_COMMIT}"; \
  CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" go build -trimpath \
    -ldflags="${ldflags}" \
    -o /out/panel ./cmd/panel; \
  verify_machine /out/panel "${TARGETARCH}"; \
  CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" go build -trimpath \
    -ldflags="${ldflags}" \
    -o /out/panel-init ./cmd/panel-init; \
  verify_machine /out/panel-init "${TARGETARCH}"

FROM --platform=$TARGETPLATFORM alpine:3.22 AS runtime-base
WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata wget \
  && addgroup -S panel \
  && adduser -S -G panel -h /app panel \
  && mkdir -p /app/data /app/web/dist \
  && chown -R panel:panel /app

ENV PANEL_LISTEN_ADDRESS=0.0.0.0:8443 \
    PANEL_DATA_ROOT=/app/data \
    PANEL_APP_DATABASE=/app/data/db/app.db \
    PANEL_METRICS_DATABASE=/app/data/db/metrics.db

EXPOSE 8443
VOLUME ["/app/data"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD wget --no-check-certificate -qO- https://127.0.0.1:8443/ >/dev/null || exit 1

FROM runtime-base AS runtime-from-artifacts
ARG TARGETOS
ARG TARGETARCH
COPY release-artifacts/panel/${TARGETOS}-${TARGETARCH}/panel /app/panel
COPY release-artifacts/panel-init/${TARGETOS}-${TARGETARCH}/panel-init /app/panel-init
COPY release-artifacts/panel-agents /app/panel-agents
COPY release-artifacts/web-dist /app/web/dist
COPY config.example.json /app/config.example.json
RUN chmod +x /app/panel /app/panel-init /app/panel-agents/*/panel-agent \
  && chown -R panel:panel /app

USER panel
ENTRYPOINT ["/app/panel-init"]

FROM runtime-base AS runtime
COPY --from=target-binaries-build /out/panel /app/panel
COPY --from=target-binaries-build /out/panel-init /app/panel-init
COPY --from=agent-bundle-build /out/panel-agents /app/panel-agents
COPY --from=web-build /src/web/dist /app/web/dist
COPY config.example.json /app/config.example.json

USER panel
ENTRYPOINT ["/app/panel-init"]
