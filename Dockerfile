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

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS backend-build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
  go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/

ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH
ARG PANEL_VERSION=dev
ARG PANEL_REPOSITORY
ARG PANEL_COMMIT
RUN set -eux; \
  test -n "${TARGETPLATFORM}"; \
  test -n "${TARGETOS}"; \
  test -n "${TARGETARCH}"; \
  case "${TARGETPLATFORM}" in \
    "${TARGETOS}/${TARGETARCH}"|"${TARGETOS}/${TARGETARCH}/"*) ;; \
    *) echo "TARGETPLATFORM=${TARGETPLATFORM} does not match ${TARGETOS}/${TARGETARCH}"; exit 1 ;; \
  esac; \
  CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" go build -trimpath \
    -ldflags="-s -w -X panel/internal/buildinfo.Version=${PANEL_VERSION} -X panel/internal/buildinfo.Repository=${PANEL_REPOSITORY} -X panel/internal/buildinfo.Commit=${PANEL_COMMIT}" \
    -o /out/panel ./cmd/panel; \
  machine="$(od -An -tx1 -j18 -N2 /out/panel | tr -d ' \n')"; \
  case "${TARGETARCH}/${machine}" in \
    amd64/3e00|arm64/b700|arm/2800|386/0300) ;; \
    *) echo "compiled /out/panel ELF machine ${machine} does not match TARGETARCH=${TARGETARCH}"; exit 1 ;; \
  esac

FROM --platform=$TARGETPLATFORM alpine:3.22 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata \
  && addgroup -S panel \
  && adduser -S -G panel -h /app panel \
  && mkdir -p /app/data /app/web/dist \
  && chown -R panel:panel /app

COPY --from=backend-build /out/panel /app/panel
COPY --from=web-build /src/web/dist /app/web/dist
COPY config.example.json /app/config.example.json

ENV PANEL_LISTEN_ADDRESS=0.0.0.0:8080 \
    PANEL_DATA_ROOT=/app/data \
    PANEL_APP_DATABASE=/app/data/db/app.db \
    PANEL_METRICS_DATABASE=/app/data/db/metrics.db

EXPOSE 8080
VOLUME ["/app/data"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD wget -qO- http://127.0.0.1:8080/ >/dev/null || exit 1

USER panel
ENTRYPOINT ["/app/panel"]
