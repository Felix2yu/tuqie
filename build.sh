#!/usr/bin/env sh
# Build the single binary: frontend first, because Go embeds web/dist.
set -e
cd "$(dirname "$0")"

pnpm --dir web install
pnpm --dir web build
go build -o bin/tuqie ./cmd/tuqie

echo "built bin/tuqie — run it with: ./bin/tuqie"
