# 图切 tuqie

把拼成长图的截图拆回一张张独立照片：自动识别拼接处的分割线，允许手动增删与拖动，
拆出来的图可以一键存进 iOS 相册，或打包成 ZIP 下载到本地。
输出的图片会保留原图的拍摄时间（读不到就用浏览器上报的文件修改时间），相册里不会全部挤成"刚刚"。
手机照片自带的旋转标记也会被读进来：你看到的，就是切出来的方向。

Go 单二进制 + 内嵌前端，没有数据库，不需要外部服务。竖着往下拼和横着往右拼的长图都会自动判方向。

![CI](https://github.com/Felix2yu/tuqie/actions/workflows/ci.yml/badge.svg)
[![codecov](https://codecov.io/gh/Felix2yu/tuqie/graph/badge.svg)](https://codecov.io/gh/Felix2yu/tuqie)

## 跑起来

```sh
./build.sh          # 先 pnpm build 再 go build，产物 bin/tuqie
./bin/tuqie         # 打开 http://localhost:7423
```

`web/pnpm-workspace.yaml` 里的 `allowBuilds: esbuild` 放行了 esbuild 的安装脚本；pnpm 11+
会因未批准的构建脚本让 `pnpm install` 直接退出码 1（实测 pnpm 11.22.0 与 12.8.1 都认这个键，
换成旧的 `onlyBuiltDependencies` 反而仍然退 1）。工具链版本只有一个来源：Node 读仓库根 `.nvmrc`，
pnpm 读 `web/package.json` 的 `packageManager`，`build.sh`、`Dockerfile` 与 CI 都不各自写死。

开发模式（前端热更新，`/api` 代理到 7423）：

```sh
go run ./cmd/tuqie              # 终端 1：后端
pnpm --dir web install && pnpm --dir web dev   # 终端 2：前端，http://localhost:5173
```

其他参数：`-addr :7423` 监听地址，`-data` 上传目录（默认 `$TMPDIR/tuqie`），
`-ttl 60m` 原图保留时长，`-password` 给整个站点加一道密码（见「放到别人够得着的地方」），
`-uploads-per-minute 30` 单个客户端每分钟能上传几张（0 为不限），
`-version` 打印版本号（CI 构建注入，本地为 `dev`）。

## 容器化部署

镜像是装配式的：统一 CI（reusable-image）先编出 `bin/tuqie`，Dockerfile 只做运行时拼装，
所以本地构建镜像前要先生成二进制（`build.sh` 已带 `-tags nodynamic`，但它产的是宿主平台的二进制，
给 Linux 镜像用还得自己交叉编译）：

```sh
./build.sh
docker build -t tuqie .
docker run -d --name tuqie -p 7423:7423 -v tuqie-data:/data tuqie
```

CI 通过后会推到 `ghcr.io/felix2yu/tuqie`。二进制是纯 Go 静态编译（CGO off 且带 `nodynamic`，原因见「图片格式」），
前端已内嵌，跑的仍是上面那个单二进制，所以参数照旧往后传就行：

```sh
docker run --rm tuqie -ttl 10m
```

`/data` 里是上传的原图，`-ttl` 到期即删，用命名卷即可、不必备份；改成宿主目录 bind mount 时，
要让容器里的非 root 用户 `tuqie` 对它可写。存相册那一步依然要求 HTTPS，见下一节。

### 放到别人够得着的地方

`-password 一道密码` 会给请求挂上 HTTP Basic 认证，页面本身也算，浏览器弹一次就记住后面所有调用；
只有 `/api/health` 放行，容器那条 `HEALTHCHECK` 带不动密码，而它回的内容只说明进程活着。
用户名不校验，只比这一道密码，比较走常数时间。限速只管 `/api/analyze`：默认每分钟 30 张，
桶是满的开始，所以开头能连着传 7 张（突发上限取每分钟额度的四分之一，不低于 6），之后每两秒回一张；
超了就 429 并带上 `Retry-After`。计的是 TCP 对端地址，挂在反向代理后面时访客都并入代理那一个，
那种部署请把限速放到代理上做。试的时候让浏览器自己弹认证框：把账号写进地址栏
（`http://user:pass@host`）虽然能打开页面，Chrome 却会拒绝这个页面发出的 fetch，上传直接报错。

## CI 与覆盖率

`.github/workflows/ci.yml` 只是薄调用层，构建 / 测试 / Codecov 上报的实现统一在
`Felix2yu/.github` 的 `reusable-test.yml`（pin 到 `@v1`）。统一规范读各仓库自己的工具链声明，
所以本仓库必须交齐三样：根目录 `.nvmrc`、`web/package.json` 的 `packageManager`、
`web/pnpm-lock.yaml`，缺任何一样 CI 直接失败。

test 通过后还有两个统一 job：`image`（reusable-image，静态编译 `bin/tuqie` 后装配进
`ghcr.io/felix2yu/tuqie`）；`release`（reusable-release，发布 Release 时产出 5 平台二进制，
以 `-ldflags "-X main.version=<tag>"` 注入版本号，供 `-version` 打印）。

覆盖率走 `go test -covermode=atomic -coverprofile=coverage.out ./...` 再交给 Codecov。
实测：全部包 91.7%（axis / detect / web 100%，split 95.2%、server 94.4%、store 91.6%）。
剩下的缺口是两类：两个 `package main` 的 flag 解析外壳（`cmd/tuqie` 65.4%、
`tools/gensample` 80.2%，逻辑都抽进了可测的 `run` / `generate`），以及写缓冲不可能失败的
防御分支（如 `zip.Create`、`png.Encode`）。闸门策略在 `codecov.yml`：project 看存量基线
（`target: auto` + informational），patch 要求新增代码 80%。本地同样一条命令就能看总数：

```sh
go test -count=1 -covermode=atomic -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -1
```

给 CI 加 `-race` 之前先看一眼 `ci.yml` 里那条注释：覆盖率插桩叠上 race 会让
1080×21600 那次 `Analyze` 跑到 8.1s，顶爆检测测试的 3s 计时预算；单独 `-race` 是过的。

Codecov 本身不参与闸门（上报失败不会让 CI 变红），但它需要先认到这个仓库：
在 codecov.io 用 GitHub 账号添加 `Felix2yu/tuqie`，然后把 token 存进仓库 secrets。

## 存到 iOS 相册

浏览器不能静默写相册，Web 上唯一的路径是系统分享面板：

- 用 Safari 打开，点「存入相册 · N 张」，在弹出的分享面板里选「存储图像」。
- 需要 HTTPS 或 localhost。局域网访问请给 `-addr` 前面挂一层 HTTPS（如 caddy），
  否则 `navigator.share` 不会启用。
- 手机拍的 HEIC 可以直接上传，不用先在相册里转成 JPEG；AVIF、JXL 同样收得下。
- 不支持文件分享的浏览器（桌面 Chrome/Safari）主按钮就是 ZIP；「逐张」是备用的散装下载，
  浏览器可能会先弹一次「是否允许下载多个文件」。

## 分割线是怎么定出来的

后端 `internal/detect` 先判方向：同一条流水线分别按行、按列各跑一遍（切割线在竖排长图里是
横线，横排长图里是竖线），哪一边给出的证据更强就用哪一边——证据是各候选得分超出 0.4 的总和，
横排要明显高过纵排 1.25 倍才翻转，因为绝大多数上传都是竖排。方向确定后：

逐行采样（每根扫描线最多读 320 个像素），对每一根线算两个量：与上一根线的平均色差 `diff`、
这一根本身的纯色程度 `flat`。候选线来自两档：

1. **留白接缝（gap）**：`diff` 突变点附近存在厚度 3–240 行、内部几乎零变化的纯色带，
   就把切割线吸附到这条留白的中心——切在这里不会切断画面内容。得分 0.55–1.0。
2. **构图突变（seam）**：没有留白时，只看突变本身还不够（照片内部的地平线、直边也会突变），
   所以再比较突变上下两块区域的平均色，构图整体变了才像拼接。得分 0.15–0.55。
   离图片两端不足一个比较窗口的突变不算构图变化，否则画面末尾的留白会被误读成"画面变了"。

之后做非极大值抑制（最小间距按图长推导），按得分排序返回。前端「灵敏度」滑杆就是这个阈值：
默认 0.45 只保留有把握的线，往下拖会露出被压住的候选（灰色细线画在强度条上），
往上拖则清空，交给用户手动画。

识别不到接缝的长图（整篇聊天记录就是这种）用「切分」那一行：等分成 N 份，或每段固定 H px，
应用后替换掉识别结果，按数值重排切割线；按钮上先写出实际会得到的张数，因为过近的线会被合并。
每条线上的编号点一下可以输入精确像素位置（"1200px" 这样的写法也认），越界或挤到相邻线时
夹到最近可用处，最小间距 30px。

切片预览每张右上角的 `−` 把它排除出这次导出，再点回来；编号只数留下的那些，所以相册里的顺序
不会留空洞。「命名」那一行管文件名：前缀默认取上传文件的名字，起始编号可以填 0 也可以填 98，
位数按最后一张自动补零（`会话-098 … 会话-100`），前缀里的 `/ \ : "` 和控制字符换成 `_`，
首尾的点号与空格会被去掉。

工作视图也不用把整张原图搬进浏览器：30MP 的长图（32MB PNG）在服务器上按盒式滤波缩成 4MP 的 JPEG，只剩 723KB，
第一次请求花 140ms 生成、之后走缓存，导出的切片仍然取自原图像素。

实测（合成图，两个方向各跑一遍）：1080×21600 约 127ms，1080×3707 约 32ms，3691×900 约 7ms；
1080×21600 单向时是 29ms，多出来的是横向扫描的代价，换来横排竖排都不用用户选。
4 张拼接的样例：纵排 807/1750/2797、横排 805/1744/2787，与生成器给出的真值都差 1px，
默认灵敏度下没有多余线。

## 接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/analyze` | multipart `file`，返回尺寸、`axis`（`y`/`x`）、候选线（`pos` 沿该轴）、强度信号 |
| GET | `/api/image?id=` | 原图，切片缩略图与工作视图都用它 |
| GET | `/api/preview?id=` | 缩放后的工作图：超过 4MP 或长边过 8192 的上传，第一次请求时按盒式滤波降采样存盘，之后直接读缓存；小图原样返回。浏览器画不出来的上传（HEIC / JXL）不管多大都渲一份 JPEG |
| GET | `/api/slice?id=&axis=&from=&to=&index=&format=&quality=` | 单张切片，`format` 取 jpeg / png / heic / avif / jxl，供分享前组装 File |
| POST | `/api/export` | `{id,axis,cuts,skip,prefix,start,format,quality}` → ZIP 流，`skip` 是要排除的切片下标 |

上传限制 250MB、像素上限 120MP，支持 PNG / JPEG / GIF / HEIC / AVIF / JXL（见「图片格式」），剪贴板里的截图可以直接 ⌘V / Ctrl+V 贴进来。

EXIF 里有两样东西会被读进来：拍摄时间写回每张切片（JPEG 走 APP1、PNG 走 eXIf 块、HEIC 走 Exif item，
AVIF 与 JXL 由我们在编码后补进容器，见「输出各档的实测」），Orientation 决定方向。带旋转标记的上传，`/api/analyze` 报的是正立后的宽高，`/api/image` 仍按原始字节发送（浏览器自己会转），
切片则是已经转正的像素——预览、缩略图与导出用的是同一个坐标系。

## 图片格式

输入认 PNG / JPEG / GIF / HEIC / AVIF / JXL，输出五选一：PNG / JPEG / HEIC / AVIF / JXL。
前三种输入走标准库，后三种各带一个纯 Go 编解码器（`gen2brain/h265`、`gen2brain/avif`、`gen2brain/jxl`）。

但纯 Go 不等于静态：`gen2brain/avif` 默认还编进一条 `purego` 的 `dlopen` 路径（`avif_dynamic.go`，tag `!nodynamic`），
它声明 libc 符号，于是 `CGO_ENABLED=0` 编出的 linux/amd64 仍带 `PT_INTERP = /lib64/ld-linux-x86-64.so.2` 与
`DT_NEEDED = libc.so.6 / libdl.so.2 / libpthread.so.0`——装配进 alpine（musl）就 execve ENOENT、容器启动即崩，
而编译和测试全程是绿的。所以 `build.sh` 与 CI 的测试、镜像构建都带 `-tags nodynamic`，只留 wazero 那条 WASM 路
（Release 的 5 平台产物没带，glibc 主机上照常能跑，musl 主机不行）：线上本来也没有
libavif 可供 dlopen，关掉它不改变任何可观测的行为，`go test -tags nodynamic ./...` 全绿。

代价写在二进制上（linux/amd64，`-trimpath -s -w`）：只带标准库时 7.7MB，加上这三个是 12.9MB（`nodynamic`，
13,476,000 字节；留着那条 dlopen 路径是 13.2MB），多出来的 5.2MB 里 AVIF 占大头——它的编解码器是编成 WASM 的
libaom，由 `wazero` 在进程里跑，这是纯 Go 路线里唯一能读懂
libheif 产出的 AVIF 的实现（另一条纯 Go 路线 `goavif` 实测读不出来）；HEIC 与 JXL 两个纯 Go 实现各约 +0.9MB。
许可随之从"只有标准库"变成 MIT ×2 加 JPEG XL Project 的 Apache-2.0（含专利授权）。

容器格式的方向不只写在 EXIF 里：HEIF/AVIF 把自己该转多少度写在 `irot`，把有效画幅写在 `clap`
（编码器按 8/16 的块对齐补边，所以解码出来的缓冲常比看到的多出一两行）。`internal/store/aperture.go`
只走文件头这几 KB，读出这两样，方向优先用 `irot`、没有才退回 EXIF Orientation，画幅在解码后按 `clap` 裁掉。
少了这一步，一张按块对齐补边的上传方向是对的、画面却整体错开一像素，切出来的每一张都跟着偏。

实测（对着第三方解码器比同一块像素）：HEIC 切片与 libheif 输出 54.7dB，JXL 与官方 `djxl` 54.2dB，
AVIF 与 `avifdec` 43.1dB（差在色度上采样的取整，不是错位）；裁掉 `clap` 之前 HEIC 只有 25.3dB。

### 输出各档的实测

滑杆留在默认的 90，一张真实照片与一张真实截图（都是 1306×1876），走 `split.Encode` 量的：

| 格式 | 照片 | 截图 | 说明 |
| --- | --- | --- | --- |
| JPEG | 243 KB / 40 ms | 361 KB / 76 ms | 谁都能开，默认档 |
| PNG | 2732 KB / 2.5 s | 1280 KB / 1.7 s | 无损 |
| HEIC | 19 KB / 0.7 s | 192 KB / 0.8 s | iOS 相册原生收，Chrome 不预览 |
| AVIF | 77 KB / 2.0 s | 157 KB / 2.6 s | Chrome 能直接显示；单张编码把进程峰值 RSS 顶到 ~600 MB（wazero 不还内存），已用一把锁串行化 |
| JXL | 74 KB / 0.5 s | 226 KB / 0.5 s | Apple 的 ImageIO 能解，Chrome 不能 |

四条表里看不见的规矩：

- **滑杆的数字不等于编码器的数字。** 各家 quality 标度不可比：直接喂进去，HEIC 在 90 档要花 992 KB 才做到 JPEG 243 KB 的观感。`split` 里给 HEIC / AVIF / JXL 各存一条曲线，五个锚点之间线性插值，取的是"这一档达到 JPEG 在同一滑杆下的 PSNR"（1306×1876 的真实照片 / 真实截图，两档内容各测一遍取平均），所以同一个位置在五种输出里是同一张观感：

  | 滑杆 | JPEG 在该档的 PSNR | HEIC | AVIF | JXL |
  | --- | --- | --- | --- | --- |
  | 60 | 40.7 / 37.3 dB | 38 | 38 | 72 |
  | 70 | 41.2 / 38.1 dB | 44 | 45 | 76 |
  | 80 | 41.8 / 39.8 dB | 52 | 56 | 83 |
  | 90 | 42.7 / 42.0 dB | 60 | 76 | 89 |
  | 100 | 51.1 / 44.9 dB | 83 | 90 | 96 |

  后三列是各编码器自己的 quality 数，不是滑杆位置；JPEG 与 PNG 不吃映射（PNG 本来就无损）。`TestEncoderQualitySpreadsTheSliderAcrossTheScales` 钉住锚点数值，并检查曲线在 60–100 之间不回头。
- **HEIC 只在两边都是偶数时用 4:2:0。** 编码器把补过边的画面按奇数的显示尺寸写进 `ispe`、却不写 conformance window，libheif 于是报"解出来的尺寸与文件声明不符"直接拒收，而用户切出来的带有一半是奇数高度；这些张改用 4:4:4（真实截图 476→600 KB，真实照片 992→1306 KB，约 +30%；噪声很重的人造图能到四倍）。`TestHeicOfAnOddSliceKeepsItsSize` 拿文件里不该出现的 `clap` 当作这个缺陷的替身。macOS 的 ImageIO 对两种都放行，libheif 只放行 4:4:4，所以按更严的一方写。
- **五种输出都带拍摄时间。** JPEG 走 APP1、PNG 走 eXIf 块、HEIC 走 Exif item；`avif` 与 `jxl` 的编码器完全不收元数据，所以导出后由 `internal/split` 往容器里补：AVIF 在 mdat 末尾接上 TIFF 并登记进 `iinf` / `iloc` / `iref`，JXL 干脆套一层容器（`JXL ` + `ftyp` + `Exif` + `jxlc`，与 `cjxl` 同形）。AVIF 的 `iref` 要按 libavif 自己的写法来——规范里那个 `reference_count` 会让它把下一个盒的头读歪，整个文件判废。实测 `avifdec` / `heif-convert` / `djxl` 都解得开，`CGImageSourceCopyPropertiesAtIndex` 对五种输出都报出同一个 `DateTimeOriginal`；ZIP 条目时间戳也按拍摄时刻写。
- `avif.Options` 的 `Speed` 没有零值兜底：`Options{Quality: 90}` 是要 libaom 最慢档（实测一张 4 分钟没出图），所以 `internal/split` 里显式写死 speed 8、JXL effort 4。

## 目录

```
cmd/tuqie        入口
internal/axis    切割方向（y 竖排 / x 横排）
internal/detect  方向判定与分割线检测
internal/exif    读写的 EXIF 字段（拍摄时间、方向）
internal/preview 工作图的盒式滤波缩放
internal/split   按轴裁切与编码
internal/store   上传文件、容器画幅与方向、解码缓存（TTL 回收）
internal/server  HTTP 接口
web/             React + Vite + Tailwind 前端，构建产物被 Go 内嵌
tools/gensample  生成合成长图，用于手动测试（`-axis x` 出横排）
Dockerfile       装配式运行镜像（COPY CI 编好的 bin/tuqie）
.github/workflows  CI，转调 Felix2yu/.github 的统一 reusable workflow（test / image / release）
codecov.yml      覆盖率闸门（规范统一，各仓库只改 paths）
```

测试：`go test ./...`；前端类型检查与构建：`pnpm --dir web build`。
