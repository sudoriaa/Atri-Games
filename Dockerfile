# syntax=docker/dockerfile:1.7

ARG NODE_VERSION=22.14.0
ARG GO_VERSION=1.26.5
ARG ALPINE_VERSION=3.22
ARG CADDY_VERSION=2.10.2

FROM node:${NODE_VERSION}-alpine AS frontend-build

ENV PNPM_HOME=/pnpm
ENV PATH=${PNPM_HOME}:${PATH}

WORKDIR /workspace

RUN corepack enable \
    && corepack prepare pnpm@10.32.1 --activate

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml .npmrc ./
COPY apps/web/package.json apps/web/package.json
COPY apps/admin/package.json apps/admin/package.json
COPY packages/shared/package.json packages/shared/package.json
COPY packages/game-kit/package.json packages/game-kit/package.json
COPY packages/game-sdk/package.json packages/game-sdk/package.json

RUN --mount=type=cache,id=atri-pnpm,target=/pnpm/store \
    pnpm install --frozen-lockfile

COPY tsconfig.base.json ./
COPY apps/web apps/web
COPY apps/admin apps/admin
COPY packages/shared packages/shared
COPY packages/game-kit packages/game-kit
COPY packages/game-sdk packages/game-sdk

ARG VITE_API_URL=/api/v1
ENV VITE_API_URL=${VITE_API_URL}

RUN pnpm --filter @atri/web build \
    && pnpm --filter @atri/admin build \
    && mkdir -p apps/web/dist/sdk \
    && cp packages/game-sdk/src/index.js apps/web/dist/sdk/atri-game-sdk.js \
    && rm -rf apps/web/dist/covers apps/web/dist/demos

FROM golang:${GO_VERSION}-alpine AS api-build

ENV PATH=/usr/local/go/bin:${PATH}

WORKDIR /workspace/apps/api

COPY apps/api/go.mod apps/api/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY apps/api ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
      -buildvcs=false \
      -trimpath \
      -ldflags="-s -w -buildid=" \
      -o /out/atri-api \
      ./cmd/server

FROM alpine:${ALPINE_VERSION} AS api

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 atri \
    && adduser -S -D -H -u 10001 -G atri atri \
    && install -d -o atri -g atri -m 0750 \
      /assets \
      /assets/covers \
      /assets/demos \
      /assets/playables \
      /data \
    && printf '%s\n' 'atri-managed-assets-v1' > /assets/.atri-managed-assets-v1 \
    && chown atri:atri /assets/.atri-managed-assets-v1

COPY --from=api-build --chown=10001:10001 /out/atri-api /usr/local/bin/atri-api
COPY --chown=10001:10001 apps/web/public/covers/ /assets/covers/
COPY --chown=10001:10001 apps/web/public/demos/ /assets/demos/

ENV ATRI_ADDR=:8080
ENV ATRI_ASSET_ROOT=/assets
ENV ATRI_DB_PATH=/data/atri-games.db

USER 10001:10001
EXPOSE 8080
STOPSIGNAL SIGTERM

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=5 \
  CMD wget -q -T 3 -O /dev/null http://127.0.0.1:8080/api/v1/health || exit 1

ENTRYPOINT ["/usr/local/bin/atri-api"]

FROM caddy:${CADDY_VERSION}-alpine AS web

RUN setcap -r /usr/bin/caddy \
    && addgroup -S -g 10001 atri \
    && adduser -S -D -H -u 10001 -G atri atri \
    && install -d -o atri -g atri -m 0750 /srv/web /srv/admin /assets /data /config \
    && chown -R atri:atri /data /config

COPY --chown=10001:10001 infra/caddy/Caddyfile /etc/caddy/Caddyfile
COPY --from=frontend-build --chown=10001:10001 /workspace/apps/web/dist /srv/web
COPY --from=frontend-build --chown=10001:10001 /workspace/apps/admin/dist /srv/admin

ENV HOME=/tmp
ENV XDG_CONFIG_HOME=/config
ENV XDG_DATA_HOME=/data
USER 10001:10001
EXPOSE 8080 8443 8443/udp
STOPSIGNAL SIGTERM

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=5 \
  CMD wget -q -T 3 -O /dev/null http://127.0.0.1:2019/config/ || exit 1
