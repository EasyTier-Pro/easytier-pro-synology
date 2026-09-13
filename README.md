# easytier-pro-synology

面向群晖 DSM 的原生 EasyTier Pro 客户端（SPK 套件，不使用 Docker）。

它把群晖 NAS 作为一台设备接入 EasyTier Console：登录 Console、取得设备注册密钥、在 NAS 上运行
`easytier-core`，并在 DSM 主菜单里提供本机管理界面与 Console 网页入口。**套件不包含 Console
服务端**，也不管理其他设备。

## 功能

- 支持 EasyTier Console 设备码登录（在浏览器完成），也支持直接粘贴设备注册令牌。
- 优先复用本机已保存的密钥，也可恢复当前设备密钥、使用已有共享密钥，或创建共享/专用密钥。
- 自动从 Console 稳定通道获取版本，优先 Gitee、回退 GitHub，只下载 Console 指定的精确 tag。
- 运行时下载带校验与事务化安装：校验压缩包成员、版本号与（若 Console 提供）摘要，
  替换过程中断或新版本不可用时会恢复旧版本。
- 连接切换使用持久事务：掉电或并发操作不会留下令牌与设置不一致的状态。
- 在界面里查看本机节点、Peer 摘要、TUN 设备和脱敏日志，并按 Console 网络列表加入/退出网络。
- 可一键打开 Console 网页控制台。
- 支持 `x86_64`、`armv8`、`armv7` 三种 DSM 架构。

## 安装

1. 在 `dist/` 中取得与 NAS 架构匹配的 `.spk`（`x86_64`、`armv8`、`armv7`）。
2. 打开 DSM「套件中心 → 手动安装」，选择该 `.spk`。
3. 安装后打开 DSM 主菜单中的「EasyTier Pro」。

首次进入页面时按引导登录 Console（或粘贴设备令牌）、选择工作空间与注册密钥；套件会自动下载并
安装 EasyTier 运行时，随后在概览页选择要加入的网络。

### 界面地址

- 页面：`/3rdparty/easytier-pro/index.html`（DSM 主菜单入口，套件注入的 nginx 配置提供），
  `/webman/3rdparty/easytier-pro/index.html` 同样可访问。
- 本机 API：`127.0.0.1:15890`，由 DSM nginx 反向代理到 `/3rdparty/easytier-pro/api/`
  与 `/webman/3rdparty/easytier-pro/api/`，浏览器与 API 同源。

## 安全模型

- 所有状态位于套件数据目录 `/volume*/@appdata/easytier-pro`，权限 `0700`，
  升级套件不会丢失，卸载时由 `postuninst` 清理。其中 `cache/` 暂存最近一次下载的运行时
  压缩包（约 25 MB）：更新在下载之后失败时，重试直接复用它而不重新下载；安装成功后立即
  删除，所以它只在"下载完成"到"更新结束"之间占用空间。
- `state/secrets/bootstrap-token`（设备注册令牌）、`state/secrets/console-session.json`
  （Console access/refresh token）、`state/machine-id` 权限均为 `0600`。
- 令牌只保存在 NAS 上，不返回浏览器；界面只读取白名单字段。
- `easytier-core` 的输出被丢弃，避免配置服务器 URL 中的注册令牌进入日志。
- 日志返回浏览器前会移除注册令牌、`Authorization` 头与 JWT。
- 本机 API 只监听回环地址，且每个请求都必须通过 DSM 会话校验：只有已登录的 DSM 管理员
  （`administrators` 组）可以调用。
- Console 会话刷新与退出在进程内串行执行；只有明确的 `invalid_grant` 才会清理会话。

## 范围限制

- 不包含 Console 服务端、relay、access、可观测性等云侧组件。
- 不管理 DSM 防火墙：虚拟网卡对局域网或反向的访问，请在
  「控制面板 → 安全性 → 防火墙」中自行放行（OpenWrt 版插件的 zone 逻辑没有移植）。
- DSM 7 强制第三方套件降权运行：`conf/privilege` 只允许 `defaults.run-as = "package"`，
  且不接受 `ctrl-script`、`executable` 与 `tool.capabilities`，因此守护进程以套件专用用户
  `easytier-pro` 运行、不带任何 capability；本机 API 只监听回环，并且每个请求都要先通过
  DSM 会话校验——守护进程携带调用方 Cookie 通过回环询问 DSM 自身的 Web API，由 DSM 判定
  该会话是否为管理员（详见 `docs/dsm-verification-checklist.md`）。
- TUN 设备需要 `CAP_NET_ADMIN`，而运行时二进制是首次连接时下载到套件数据目录的，无法由 DSM
  提前授予。没有该能力时守护进程会自动把本机节点切换为**无 TUN 模式**（在 EasyTier Console 上
  设置该节点的 `no_tun`，使下发的实例配置与本机权限一致）。无 TUN 模式并不等于退出网络：
  本机仍会获得虚拟 IP，其它节点可以主动访问本机（也能经本机虚拟 IP 访问本机上的服务），
  子网路由与中继照常可用；唯一的区别是本机自身没有虚拟网卡，NAS 上的程序无法主动访问网络中的
  其它节点。概览页会显示「无 TUN 模式（用户态转发）」并说明原因。
- 连接同样不需要能力：core 默认会把出站 socket 绑定到网卡（需要 `CAP_NET_RAW`），守护进程会
  在 Console 上为本机节点关闭该绑定（`bind_device = false`）。因此**在无 TUN 模式下本套件不需要
  任何 capability，也不需要任何 root 操作**。若希望 NAS 本身拥有虚拟网卡（即让 NAS 上的程序能
  主动访问其它节点），可由管理员可选地授予 `CAP_NET_ADMIN`（`setcap` 命令见概览页）。
- 需要本机拥有虚拟 IP 时，由管理员一次性授予该文件能力，然后重新启动套件：
  `sudo setcap cap_net_admin,cap_net_raw+ep /volume*/@appdata/easytier-pro/runtime/easytier-core`
  （每次重新下载运行时后需要再执行一次；概览页的「复制命令」按钮直接给出当前路径的完整命令）。
  守护进程每次启动都会重新检查该能力：检测到后会自动取消 Console 上的 `no_tun` 设置，恢复完整模式。
- 界面只覆盖本机相关操作，完整的 Console 管理能力请打开 Console 网页。

## 开发

```sh
go vet ./... && go test ./...          # 静态检查与单元测试
scripts/build-spk.sh --arch all        # 构建三个架构的 SPK（会先构建界面）
scripts/check-spk.sh dist/*.spk        # 校验 SPK 结构
```

界面是构建产物，守护进程用 `go:embed` 内嵌 `ui/dist/`，所以改界面后必须重新构建它，
否则 Go 侧编译或运行到的仍是上一次的产物：

```sh
cd ui
npm ci
npm test                               # 组件与工具函数测试（vitest）
npm run dev                            # 本地开发，接口默认指向同源
npm run build                          # 产出 ui/dist/，提交进仓库供 go:embed 使用
```

`ui/dist/` 之所以提交，是因为内嵌发生在编译期：只装 Go 工具链的人也要能构建出可用的
守护进程。改动界面源码后请一并提交重新构建的 `ui/dist/`。

守护进程每 30 分钟向 Console 查一次当前稳定版本。装好的核心比它旧时，界面会提示并
提供一键升级；升级由用户确认后执行（会重启核心、短暂断网），不会自动进行。同一份版本
判定逻辑既用于校验下载到的二进制，也用于判断是否有新版本，见 `versionReports`。

界面默认跟随系统明暗，页头可切换为固定浅色或深色，选择保存在浏览器里。首屏背景由
`ui/index.html` 的内联样式按同一选择直接画出，不等 JS 加载，因此打开时不会先白一下；
该文件的 `data-theme` 属性由 `ui/src/theme.ts` 维护，两处必须保持一致。

`ui/index.html` 里的内联样式是刻意为之，不要挪进打包产物。

在没有 DSM 的机器上调试（仅当设置了 `ETP_DEV_ROOT` 时下列变量才会生效）：

```sh
ETP_DEV_ROOT=/tmp/etp ETP_DEV_NO_DSM_AUTH=1 ETP_DEV_LISTEN=0.0.0.0:15890 \
	./easytier-pro-dsm serve
```

`ETP_DEV_ROOT` 会把套件目录重定位到 `<root>/target` 与 `<root>/var`，此时
`ETP_DEV_NO_DSM_AUTH=1` 跳过 DSM 会话校验，`ETP_DEV_LISTEN` 改写监听地址。三者都不会在
DSM 上生效。

图标由 Console 的品牌图形生成（一次性提交，不参与构建）：

```sh
for n in 16 24 32 48 64 72 256; do
	rsvg-convert -w $n -h $n -o ui/public/images/app_$n.png ../easytier-console/web/public/favicon.svg
done
rsvg-convert -w 64 -h 64 -o spk/icons/PACKAGE_ICON.PNG ../easytier-console/web/public/favicon.svg
rsvg-convert -w 256 -h 256 -o spk/icons/PACKAGE_ICON_256.PNG ../easytier-console/web/public/favicon.svg
```

## 目录结构

|路径|说明|
|---|---|
|`cmd/easytier-pro-dsm`|入口：`supervise`（套件启停）、`serve`（守护进程）、`version`|
|`internal/config`|包路径、设置、密钥、机器标识、原子写与日志|
|`internal/console`|EasyTier Console 客户端（设备码登录、会话、注册密钥、网络与节点）|
|`internal/runtime`|`easytier-core` 监督、运行时下载与事务化更新、连接切换|
|`internal/httpserver`|本机 REST API 与静态界面|
|`internal/dsmenv`|DSM 会话校验|
|`ui/`|管理界面（Vue 3 + TypeScript + Vite；`npm run build` 产物提交在 `ui/dist/`，因为守护进程用 `go:embed` 内嵌它）|
|`spk/`|套件元数据、脚本与 nginx 注入配置|
