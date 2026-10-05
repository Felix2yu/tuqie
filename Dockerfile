# syntax=docker/dockerfile:1

# Frontend first: the Go binary embeds web/dist, so the static assets have to
# exist before the compile step. Same order as ./build.sh.
FROM node:24-alpine AS web
WORKDIR /src/web

# Resolved before the sources are copied, because the lockfile changes far less
# often than the code.
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./

# Pinned to 12 rather than left to whatever image default: pnpm 11+ turns an
# unapproved dependency build script into `pnpm install` exit 1, and the
# `allowBuilds` key that approves esbuild only exists in pnpm 12.
RUN npm install --global pnpm@12.8.1 && pnpm install --frozen-lockfile

COPY web/ ./
RUN pnpm build

FROM golang:1.27-alpine AS build
WORKDIR /src

COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
COPY web/embed.go ./web/
COPY --from=web /src/web/dist ./web/dist

# Zero third-party modules, so no `go mod download` layer is needed. CGO off keeps
# the binary static, which is what lets the last stage be a bare alpine.
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/tuqie ./cmd/tuqie

FROM alpine:3.22
RUN addgroup --system tuqie \
    && adduser --system --ingroup tuqie tuqie \
    && mkdir -p /data \
    && chown tuqie:tuqie /data

COPY --from=build /out/tuqie /usr/local/bin/tuqie

USER tuqie
EXPOSE 7423

ENTRYPOINT ["/usr/local/bin/tuqie"]
# Flags override as arguments: `docker run ... tuqie -ttl 10m`
CMD ["-addr", ":7423", "-data", "/data", "-ttl", "60m"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
    CMD wget -qO- http://127.0.0.1:7423/api/health >/dev/null || exit 1
