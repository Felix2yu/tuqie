---
name: tuqie-server-deploy-env
description: tuqie 线上跑在他自己的 x86_64 服务器上，由一个未知面板部署，compose 配置与仓库里那份不一致——仓库 compose 不能当线上真相
metadata:
  type: project
---

线上（2026-10-08 实测确认）：`uname -m` = x86_64，`docker version` = `28.5.2+dfsg4`（Debian 包 docker.io，非官方 static build），镜像来自 `ghcr.io/felix2yu/tuqie:latest`。

线上那份 compose 和仓库里的 `docker-compose.yml` **不一样**：

- 卷：线上是命名卷 `tuqie-data`（早期还出现过 bind `/app/tuqie/data:/data`），仓库那份是匿名命名卷。
- 线上多了 `container_name: tuqie`、`user: "1000:100"`、`TZ=Asia/Shanghai`、`networks: [proxy]`（external，`name: proxy`），没有 `ports`——走前面的反代。
- 日志前缀形如 `2026-10-08 10:19:37 [tuqie] <容器原始输出>`，外层是面板加的（`undefined` 那截像是 JS 模板变量），**面板到底是哪个他没答**（Coolify / Dokploy / 1Panel 都可能）。

**Why:** 他一个人运维，线上编排不落在仓库里，只在服务器和面板上。

**How to apply:** 涉及镜像、启动、挂载的问题，先分清三层：本机的他（没 docker）/ 他 shell 里的 docker CLI / 面板实际用的 daemon。要结论就拿服务器上的 `docker inspect` / `docker image inspect` 输出对照，别拿仓库 compose 推断线上行为；发现了仓库与线上的不一致，提出来让他定夺（改 compose 还是改线上），不要擅自统一。诊断阶段给「编号 + 只读」命令清单，他会照跑并贴回原样输出。

相关：[[tuqie-docker-exec-enoent-open]]、[[verify-perf-claims-with-numbers]]、[[github-org-reusable-workflows]]
