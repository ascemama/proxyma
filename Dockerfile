# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:26-alpine AS frontend
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./frontend/
RUN cd frontend && npm ci
COPY frontend ./frontend
RUN cd frontend && npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.2-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
ARG BUILD_VERSION=dev
ARG BUILD_COMMIT=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=frontend /src/frontend/dist ./internal/web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -tags embedded_frontend -trimpath -ldflags="-s -w -X proxyma/internal/buildinfo.rawVersion=$BUILD_VERSION -X proxyma/internal/buildinfo.rawCommit=$BUILD_COMMIT" -o /out/proxyma ./cmd/proxyma
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/proxymactl ./cmd/proxymactl

FROM alpine:3.24
LABEL org.opencontainers.image.title="Proxyma" \
  org.opencontainers.image.description="Policy-enforcing proxy for AI agent access to remote systems" \
  org.opencontainers.image.source="https://github.com/ascemama/proxyma" \
  org.opencontainers.image.licenses="AGPL-3.0-only"
RUN apk upgrade --no-cache \
  && apk add --no-cache ca-certificates tzdata \
  && addgroup -S -g 10001 proxyma \
  && adduser -S -G proxyma -u 10001 proxyma \
  && mkdir -p /etc/proxyma /var/lib/proxyma /usr/share/licenses/proxyma \
  && chown -R proxyma:proxyma /etc/proxyma /var/lib/proxyma
COPY --from=builder /out/proxyma /usr/local/bin/proxyma
COPY --from=builder /out/proxymactl /usr/local/bin/proxymactl
COPY LICENSE /usr/share/licenses/proxyma/LICENSE
USER proxyma
WORKDIR /var/lib/proxyma
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/proxyma"]
CMD ["--config", "/etc/proxyma/proxyma.config.json"]
