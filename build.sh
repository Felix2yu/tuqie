#!/usr/bin/env sh
# Build the single binary: frontend first, because Go embeds web/dist.
set -e
cd "$(dirname "$0")"

pnpm --dir web install
pnpm --dir web build
# nodynamic：关掉 gen2brain/avif 的 purego dlopen 绑定。不带它时，即便 CGO_ENABLED=0，
# Linux 产物仍带 glibc 的 PT_INTERP，装进 alpine（musl）启动即 ENOENT；CI 编镜像同带此 tag。
go build -tags nodynamic -o bin/tuqie ./cmd/tuqie

echo "built bin/tuqie — run it with: ./bin/tuqie"
