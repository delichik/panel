# syntax=docker/dockerfile:1.7

FROM node:22-alpine AS web-build
WORKDIR /src/web

COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
RUN npm run build

FROM golang:1.25-alpine AS backend-build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/

ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/panel ./cmd/panel

FROM alpine:3.22 AS runtime
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
