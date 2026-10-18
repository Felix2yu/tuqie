# syntax=docker/dockerfile:1

# 装配式镜像：二进制由 CI（reusable-image）预编译进 bin/tuqie 后 COPY。
# 前端已经通过 go:embed 内嵌进二进制，镜像内不再需要 Node 或 Go 阶段。
# 本地 `docker build` 前先跑 ./scripts/build.sh 产出 bin/tuqie。
#
# 基镜 ghcr.io/felix2yu/base:alpine-3.24 提供 ca-certificates、tzdata、su-exec
# 与 PUID/PGID 降权入口；原先的 `USER tuqie` 由基镜入口的降权取代。
FROM ghcr.io/felix2yu/base:alpine-3.24

RUN mkdir -p /data

COPY --chmod=755 bin/tuqie /usr/local/bin/tuqie

ENV APP_DATA_DIR=/data

EXPOSE 7423

# Only the path that has no built-in default is spelled out here; -addr and -ttl
# already default to :7423 and 60m, and naming them in the image would outrank the
# TUQIE_ADDR / TUQIE_TTL a compose file sets, because a flag beats an environment
# variable. 注意 CMD 现在是完整命令：ENTRYPOINT 换成了基镜的降权入口，
# 不再是二进制本身，所以可执行文件必须写进 CMD。
CMD ["/usr/local/bin/tuqie", "-data", "/data"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
    CMD wget -qO- http://127.0.0.1:7423/api/health >/dev/null || exit 1
