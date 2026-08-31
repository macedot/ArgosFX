# Caddy with the cache-handler plugin for HTTP response caching.
FROM caddy:2-builder AS build
RUN xcaddy build \
    --with github.com/caddyserver/cache-handler \
    --with github.com/mholt/caddy-ratelimit

FROM caddy:2
COPY --from=build /usr/bin/caddy /usr/bin/caddy
