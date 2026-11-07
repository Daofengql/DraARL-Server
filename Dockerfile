# syntax=docker/dockerfile:1.7

FROM node:24.22.2-alpine3.24 AS frontend
WORKDIR /src/www
COPY www/package.json www/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY www/ ./
ARG DRAARL_VERSION=dev
RUN VITE_APP_VERSION="${DRAARL_VERSION}" npm run build

FROM golang:1.27.3-alpine3.24 AS backend
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . ./
COPY --from=frontend /src/www/dist ./internal/server/web/dist
ARG DRAARL_VERSION=dev
ARG GO_BUILD_PARALLELISM=2
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)" && \
    CGO_ENABLED=0 GOMAXPROCS=${GO_BUILD_PARALLELISM} go build -p=${GO_BUILD_PARALLELISM} -tags=embed \
      -ldflags="-s -w -X draarl/internal/buildinfo.Version=${DRAARL_VERSION} -X draarl/internal/buildinfo.BuildTime=${BUILD_TIME} -X draarl/internal/buildinfo.Release=true" \
      -o /out/draarl ./cmd/draarl

FROM alpine:3.24
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates gettext-envsubst tzdata ffmpeg && \
    addgroup -S -g 10001 draarl && \
    adduser -S -D -H -u 10001 -G draarl draarl && \
    mkdir -p /var/lib/draarl && \
    chown -R draarl:draarl /var/lib/draarl
COPY --from=backend /out/draarl /usr/local/bin/draarl
COPY deploy/docker/config.yaml.template /etc/draarl/config.yaml.template
COPY deploy/docker/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
COPY deploy/docker/healthcheck.sh /usr/local/bin/docker-healthcheck.sh
RUN chmod 0755 /usr/local/bin/draarl /usr/local/bin/docker-entrypoint.sh /usr/local/bin/docker-healthcheck.sh

USER draarl
WORKDIR /var/lib/draarl
ENV TZ=Asia/Shanghai
EXPOSE 60050/udp 9000/tcp 60100/tcp
VOLUME ["/var/lib/draarl"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 CMD ["/usr/local/bin/docker-healthcheck.sh"]
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
