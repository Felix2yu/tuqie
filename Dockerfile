# syntax=docker/dockerfile:1

# 装配式镜像：二进制由 CI（reusable-image）预编译进 bin/tuqie 后 COPY。
# 前端已经通过 go:embed 内嵌进二进制，镜像内不再需要 Node 或 Go 阶段。
# 本地 `docker build` 前先跑 ./build.sh 产出 bin/tuqie。
FROM alpine:3.24

RUN addgroup --system tuqie \
    && adduser --system --ingroup tuqie tuqie \
    && mkdir -p /data \
    && chown tuqie:tuqie /data

COPY --chmod=755 bin/tuqie /usr/local/bin/tuqie

USER tuqie
EXPOSE 7423

ENTRYPOINT ["/usr/local/bin/tuqie"]
# Only the path that has no built-in default is spelled out here; -addr and -ttl
# already default to :7423 and 60m, and naming them in the image would outrank the
# TUQIE_ADDR / TUQIE_TTL a compose file sets, because a flag beats an environment
# variable.
CMD ["-data", "/data"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
    CMD wget -qO- http://127.0.0.1:7423/api/health >/dev/null || exit 1
