# 图切 tuqie

把拼成长图的截图拆回一张张独立照片：自动识别拼接处的分割线，允许手动增删与拖动，
拆出来的图可以一键存进 iOS 相册，或打包成 ZIP 下载到本地。

Go 单二进制 + 内嵌前端，没有数据库，不需要外部服务。竖着往下拼和横着往右拼的长图都会自动判方向。

## 跑起来

```sh
./build.sh          # 先 pnpm build 再 go build，产物 bin/tuqie
./bin/tuqie         # 打开 http://localhost:7423
```

`web/pnpm-workspace.yaml` 放行了 esbuild 的安装脚本；pnpm 11+ 对未批准的构建脚本
会直接让 `pnpm install` 退出码 1。

开发模式（前端热更新，`/api` 代理到 7423）：

```sh
go run ./cmd/tuqie              # 终端 1：后端
pnpm --dir web install && pnpm --dir web dev   # 终端 2：前端，http://localhost:5173
```

其他参数：`-addr :7423` 监听地址，`-data` 上传目录（默认 `$TMPDIR/tuqie`），
`-ttl 60m` 原图保留时长。

## 容器化部署

```sh
docker build -t tuqie .
docker run -d --name tuqie -p 7423:7423 -v tuqie-data:/data tuqie
```

多阶段构建：node 阶段产出 `web/dist`，go 阶段把它内嵌进二进制，最后一层只有 alpine 加一个文件，
跑的仍是上面那个单二进制，所以参数照旧往后传就行：

```sh
docker run --rm tuqie -ttl 10m
```

`/data` 里是上传的原图，`-ttl` 到期即删，用命名卷即可、不必备份；改成宿主目录 bind mount 时，
要让容器里的非 root 用户 `tuqie` 对它可写。存相册那一步依然要求 HTTPS，见下一节。

## 存到 iOS 相册

浏览器不能静默写相册，Web 上唯一的路径是系统分享面板：

- 用 Safari 打开，点「存入相册 · N 张」，在弹出的分享面板里选「存储图像」。
- 需要 HTTPS 或 localhost。局域网访问请给 `-addr` 前面挂一层 HTTPS（如 caddy），
  否则 `navigator.share` 不会启用。
- 从相册选图上传时，iOS 会自动把 HEIC 转成 JPEG，后端不需要解码 HEIC。
- 不支持文件分享的浏览器（桌面 Chrome/Safari）会自动退化成逐张下载，右侧 ZIP 按钮始终可用。

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

实测（合成图，两个方向各跑一遍）：1080×21600 约 127ms，1080×3707 约 32ms，3691×900 约 7ms；
1080×21600 单向时是 29ms，多出来的是横向扫描的代价，换来横排竖排都不用用户选。
4 张拼接的样例：纵排 807/1750/2797、横排 805/1744/2787，与生成器给出的真值都差 1px，
默认灵敏度下没有多余线。

## 接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/analyze` | multipart `file`，返回尺寸、`axis`（`y`/`x`）、候选线（`pos` 沿该轴）、强度信号 |
| GET | `/api/image?id=` | 原图，前端预览与切片缩略图都用它 |
| GET | `/api/slice?id=&axis=&from=&to=&index=&format=&quality=` | 单张切片，供分享前组装 File |
| POST | `/api/export` | `{id,axis,cuts,format,quality}` → ZIP 流 |

上传限制 250MB、像素上限 120MP，支持 PNG / JPEG / GIF。

## 目录

```
cmd/tuqie        入口
internal/axis    切割方向（y 竖排 / x 横排）
internal/detect  方向判定与分割线检测
internal/split   按轴裁切与编码
internal/store   上传文件与解码缓存（TTL 回收）
internal/server  HTTP 接口
web/             React + Vite + Tailwind 前端，构建产物被 Go 内嵌
tools/gensample  生成合成长图，用于手动测试（`-axis x` 出横排）
```

测试：`go test ./...`；前端类型检查与构建：`pnpm --dir web build`。
