# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:22-alpine AS build

WORKDIR /src

COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./

ARG PACTLINE_IDENTITY_MODE=lark
RUN VITE_AUTH_PROVIDER=$PACTLINE_IDENTITY_MODE npm run build

FROM caddy:2-alpine

RUN addgroup -S -g 10001 pactline \
    && adduser -S -D -H -u 10001 -G pactline pactline \
    && cp /usr/bin/caddy /usr/local/bin/caddy-unprivileged

COPY deploy/Caddyfile /etc/caddy/Caddyfile
COPY --from=build /src/dist /srv

LABEL org.opencontainers.image.source="https://github.com/wolfhead/pactline"

USER pactline
EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=15s --retries=5 \
    CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/caddy-unprivileged"]
CMD ["run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
