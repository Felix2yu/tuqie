#!/usr/bin/env bash
# 土锲构建脚本 —— 先构建前端（Go 内嵌 web/dist），再编单二进制。
#
# 产物：bin/tuqie
# 前置：Go + Node（大版本见 .nvmrc）+ pnpm
#
# 用法：
#   ./scripts/build.sh
#   GO_BIN=/path/to/go NODE_BIN=/path/to/node ./scripts/build.sh
#
# 约定见 Felix2yu/.github/docs/build-script.md
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

step() { printf '\033[1;34m==>\033[0m %s\n' "$1"; }
die() { printf '\033[1;31m错误：\033[0m %s\n' "$1" >&2; exit 1; }

# ---------- Node（大版本从 .nvmrc 读，脚本里不写死版本号）----------
node_major() { "$1" -v 2>/dev/null | sed 's/^v\([0-9]*\).*/\1/'; }

REQ_MAJOR="$(sed -n 's/^v\{0,1\}\([0-9][0-9]*\).*/\1/p' "$ROOT/.nvmrc" | head -1)"
[ -n "$REQ_MAJOR" ] || die "读不出 $ROOT/.nvmrc 里的 Node 大版本"

node_candidates() {
  command -v node 2>/dev/null || true
  ls -1d "${NVM_DIR:-$HOME/.nvm}"/versions/node/v*/bin/node 2>/dev/null | sort -Vr || true
  echo "/opt/homebrew/opt/node@${REQ_MAJOR}/bin/node"
  echo "/opt/homebrew/opt/node/bin/node"
  echo "/usr/local/opt/node@${REQ_MAJOR}/bin/node"
  echo "/usr/local/opt/node/bin/node"
}

pick_node() {
  local want="$1" cand maj
  while IFS= read -r cand; do
    [ -n "$cand" ] && [ -x "$cand" ] || continue
    maj="$(node_major "$cand")"
    [ -n "$maj" ] || continue
    if [ "$want" = exact ]; then
      [ "$maj" = "$REQ_MAJOR" ] && { printf '%s\n' "$cand"; return 0; }
    else
      [ "$maj" -gt "$REQ_MAJOR" ] && { printf '%s\n' "$cand"; return 0; }
    fi
  done < <(node_candidates)
  return 1
}

if [ -z "${NODE_BIN:-}" ]; then
  NODE_BIN="$(pick_node exact || pick_node newer || true)"
fi

if [ -z "${NODE_BIN:-}" ] || [ ! -x "${NODE_BIN:-}" ]; then
  die "找不到 Node ${REQ_MAJOR}（当前 PATH 上是 $(node -v 2>/dev/null || echo 无)）。
     请安装：nvm install ${REQ_MAJOR}，或显式指定：NODE_BIN=/path/to/node $0"
fi

ACTUAL_MAJOR="$(node_major "$NODE_BIN")"
[ -n "$ACTUAL_MAJOR" ] || die "无法识别 $NODE_BIN 的版本"
[ "$ACTUAL_MAJOR" -ge "$REQ_MAJOR" ] || \
  die "Node 大版本过低：.nvmrc 要求 ${REQ_MAJOR}，$NODE_BIN 是 ${ACTUAL_MAJOR}"

export PATH="$(dirname "$NODE_BIN"):$PATH"

# ---------- Go ----------
if [ -z "${GO_BIN:-}" ]; then
  if command -v go >/dev/null 2>&1; then
    GO_BIN="$(command -v go)"
  elif [ -x /opt/homebrew/bin/go ]; then
    GO_BIN=/opt/homebrew/bin/go
  elif [ -x /usr/local/go/bin/go ]; then
    GO_BIN=/usr/local/go/bin/go
  else
    die "找不到 go，可显式指定：GO_BIN=/path/to/go $0"
  fi
fi
[ -x "$GO_BIN" ] || die "GO_BIN 不可执行：$GO_BIN"

PNPM_BIN="${PNPM_BIN:-$(command -v pnpm || true)}"
[ -n "$PNPM_BIN" ] && [ -x "$PNPM_BIN" ] || \
  die "找不到 pnpm（安装：npm i -g pnpm；版本由 web/package.json 的 packageManager 决定）"

step "[1/3] 安装前端依赖"
"$PNPM_BIN" --dir web install --frozen-lockfile

step "[2/3] 构建前端（产物直接被 web/embed.go 内嵌）"
"$PNPM_BIN" --dir web run build

# nodynamic：关掉 gen2brain/avif 的 purego dlopen 绑定。不带它时，即便 CGO_ENABLED=0，
# Linux 产物仍带 glibc 的 PT_INTERP，装进 alpine（musl）启动即 ENOENT；CI 编镜像同带此 tag。
step "[3/3] 构建后端（go build -tags nodynamic）"
mkdir -p "$ROOT/bin"
CGO_ENABLED=0 "$GO_BIN" build -tags nodynamic -trimpath -ldflags "-s -w" \
  -o "$ROOT/bin/tuqie" ./cmd/tuqie

printf '\n完成：%s\n' "$ROOT/bin/tuqie"
printf '运行：cd %s && ./bin/tuqie\n' "$ROOT"
