# tuqie —— 目标约定见 Felix2yu/.github/docs/makefile.md
#
# 七个必备目标：build / dev / test / lint / fmt / docker / clean
# 其余目标按仓库自身需要添加。
.PHONY: build dev test lint lint-strict fmt docker clean

BIN  := bin/tuqie
MAIN := ./cmd/tuqie

## 产出 $(BIN)。前端构建产物在 go build 前摆进 go:embed 的位置，实现集中在 scripts/build.sh
build:
	./scripts/build.sh

## 本地起服务；前端热更新另开终端：cd web && pnpm run dev
dev:
	go run $(MAIN)

test:
	go test ./...

## 静态检查：go vet + 前端类型检查（两者都应为零告警）
lint:
	go vet ./...
	cd web && pnpm run typecheck

## 深度检查：在 lint 之上加 golangci-lint（存量告警未清零前会红，故不并入 lint）
lint-strict: lint
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run; else echo "  （未安装 golangci-lint，跳过）"; fi

## 格式化：只改 Go 源码，不碰生成物
fmt:
	gofmt -w $$(git ls-files '*.go')

## 装配式镜像：先出二进制，镜像只负责 COPY
docker: build
	docker build -t tuqie:dev .

clean:
	rm -rf bin web/dist
