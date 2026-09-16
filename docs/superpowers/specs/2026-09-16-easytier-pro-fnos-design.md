# EasyTier Pro fnOS 应用设计

日期：2026-09-16
状态：已评审（2026-09-16 修订：改为 monorepo 架构 + 真机 E2E 交付门槛）
目标仓库：/data/project/easytier-pro-synology（monorepo，synology 与 fnos 共用）

## 1. 背景与目标

为 EasyTier Pro 做一个飞牛 NAS（fnOS）应用，功能与 Synology 版完全对齐：
设备登录授权（device flow）、工作区激活、加入/退出网络、节点状态、日志
查看、easytier-core 运行时自动下载与更新。UI 样式不强制与群晖版一致。

**交付门槛：必须在真实 fnOS 系统（PVE VM）上完成完整新用户使用流程的
E2E 测试后才可以交付。**

**monorepo 决策（用户 2026-09-16 提出）：** 移植过程中已验证 console、
runtime、config 等核心包在两平台间 95%+ 逐字节相同，独立仓库必然产生
双向同步负担。因此改为单仓库双出包：synology 版继续产出 `.spk`，fnOS 版
产出 `.fpk`，共享 `internal/` 全部业务代码与一份 UI 源码，平台差异收敛到
薄适配层。目标仓库沿用 `/data/project/easytier-pro-synology`（保留 git
历史、CI、release 流），`easytier-pro-fnos` 独立仓库废弃。

fnOS 官方提供完整的第三方应用开发文档（developer.fnnas.com）：应用以 `.fpk`
包分发（tar.gz 结构 + INI 风格 manifest），支持 root 权限应用，提供"统一网关"
（fnOS nginx 校验 NAS 登录态后经 Unix socket 反代到应用，并注入
`X-Trim-Userid` / `X-Trim-Username` / `X-Trim-Isadmin` 身份头，支持 WebSocket；
身份头能力要求 fnOS ≥ 1.2.0401）。fnOS 基于 Debian，/dev/net/tun 可用。
架构支持 x86_64 与 arm64。

## 2. 关键决策

| 决策点 | 选择 | 理由 |
|---|---|---|
| 功能范围 | 对齐 Synology 版全量功能 | 用户已确认 |
| 权限模型 | `run-as: root` | VPN 需要 TUN/NET_ADMIN；fnOS 官方允许 root 应用（Clash-for-fnos 等同类均如此）；不做 DSM7 式 no_tun 降级 |
| UI 集成 | fnOS 统一网关（gatewayPrefix + gatewaySocket） | 官方推荐方式；自动处理登录态校验；支持 WebSocket；无需自带 nginx 配置 |
| 管理 API 鉴权 | 校验网关注入的 `X-Trim-Isadmin: true` | 对齐 Synology 版的"仅 NAS 管理员可用"语义 |
| easytier-core 获取 | 运行时从 Console 发布信息下载，不打进包 | 所有现有客户端的统一做法；core 版本由 Console `stable.version` 统一定 |
| 默认 Console | `https://api.console.easytier.net` | 与其他客户端一致 |
| 架构 | x86_64 + arm64 两个 fpk | fnOS 双架构；Go 交叉编译成本低 |
| 安装向导 | 无 wizard | 与 Synology 版一致：安装后在应用 UI 内完成配置 |
| 技术栈 | Go 1.24 零依赖 daemon + Vue3/TS/Vite UI | 直接移植 Synology 版已验证的栈 |

### 候选方案对比

- **A. 移植 Synology 架构（采纳）**：新仓库复制 synology 版的 daemon/UI/构建脚本，
  替换平台相关层（环境探测、会话校验、打包）。架构已被 Synology 与 OpenWrt 两个
  平台验证，落地最快。
- B. 抽取共享 Go module：长期更优但要改动 synology 仓库，超出本任务范围，留作
  后续演进方向。
- C. Docker 型 fpk：TUN + host 网络下容器化体验差，且与现有客户端架构不一致，
  放弃。

## 3. 总体架构

```
fnOS 桌面图标 → fnOS nginx（校验 NAS 登录态，注入 X-Trim-* 头）
      │ 反代 /app/easytier-pro/** （HTTP + WebSocket）
      ▼ Unix socket: $TRIM_APPDEST/app.sock
easytier-pro-fnos daemon（root，supervise/serve 双进程）
      ├── 内嵌 Vue UI（go:embed），提供 REST API
      ├── 状态/密钥：$TRIM_PKGVAR/state/（secrets 0600）
      ├── 运行时管理：$TRIM_PKGVAR/runtime/easytier-core（Console 定版下载，
      │   Gitee 优先 GitHub 兜底，sha256 校验，事务式更新 + 回滚）
      └── 拉起 easytier-core --secure-mode=true，env 注入配置
```

core 启动契约（与 synology/luci 完全一致）：

```
ET_CONFIG_SERVER=<config_server>/<bootstrap_token>
ET_MACHINE_ID=</etc/machine-id 内容，缺失时用持久化的随机 id>
ET_HOSTNAME=<hostname>
ET_RPC_PORTAL=0.0.0.0:15888
ET_RPC_PORTAL_WHITELIST=127.0.0.0/8,::1/128
ET_CONSOLE_LOG_LEVEL=off
easytier-core --secure-mode=true   # stdout/stderr 丢弃（URL 含 token）
```

注：RPC portal 沿用 synology 的 0.0.0.0 监听 + loopback 白名单形式
（synology 代码注释说明绑 lo 需要 CAP_NET_RAW，且该形态已验证）；
本应用虽有 root，但保持与已验证形态一致，不单独改绑 127.0.0.1。

健康检查 = TCP 拨测 RPC portal；状态查询 = `easytier-cli -p 127.0.0.1:15888`。

## 4. 仓库布局（monorepo）

```
easytier-pro-synology/                  # monorepo 根（沿用现有仓库与历史）
├── cmd/
│   ├── easytier-pro-dsm/main.go        # 群晖入口（supervise|serve|version）
│   └── easytier-pro-fnos/main.go       # fnOS 入口（同结构，差异仅在适配层）
├── internal/
│   ├── apperr/                         # 共享：错误码目录（含 dsm_auth_* 与 fnos_auth_*）
│   ├── config/                         # 共享：Store/Logger/原子写；路径根解析按平台拆 paths_dsm.go/paths_fnos.go
│   ├── console/                        # 共享：Console REST 客户端（品牌名经 platform.DisplayName 注入）
│   ├── runtime/                        # 共享：core 生命周期、事务式下载、relay 同步
│   ├── httpserver/                     # 共享：路由、Origin/CSRF 检查、静态资源；鉴权中间件接受
│   │                                   #   Authenticator 接口 + 平台错误码对，requireSession 泛化
│   ├── dsmenv/                         # 群晖：DSM 会话探测（现状不变）
│   ├── fnosenv/                        # fnOS：X-Trim-* 身份头校验（新写）
│   ├── daemon/                         # 共享：supervise/serve 脚手架，平台注入监听器与鉴权器
│   └── platform/                       # 平台常量：DisplayName、监听器工厂、鉴权器工厂
│       ├── dsm.go                      #   "Synology NAS"、TCP 127.0.0.1:15890、dsmenv
│       └── fnos.go                     #   "fnOS NAS"、Unix socket $TRIM_APPDEST/app.sock、fnosenv
├── ui/                                 # 单份 Vue3 源码，平台差异经 VITE_PLATFORM 构建期注入
│   ├── src/platform/{dsm,fnos}.ts      #   平台配置：base、鉴权头、错误码、重登链接、品牌文案
│   ├── dist-dsm/                       #   群晖构建产物（入库，embed）
│   └── dist-fnos/                      #   fnOS 构建产物（入库，embed）
├── spk/                                # 群晖打包（现状不变）
├── fpk/                                # fnOS 打包（新增，结构见 §9）
├── scripts/                            # build-spk.sh / build-fpk.sh / check-* / test-*
├── docs/
└── dist/                               # 构建产物（*.spk 与 *.fpk）
```

模块名沿用 `github.com/EasyTier-Pro/easytier-pro-dsm`（不动，避免全仓 import
churn；module 名只是构建标识，仓库同时产出两个平台二进制）。版本号各自维护：
spk/INFO 与 fpk/manifest 独立，构建时各自 `-X main.buildVersion` 注入。fnOS 包
初始版本 `0.1.0`。

## 5. 平台适配层（fnosenv）

替代 synology 的 dsmenv，负责：

- 读取 `TRIM_APPDEST` / `TRIM_PKGETC` / `TRIM_PKGVAR` / `TRIM_PKGTMP` /
  `TRIM_APPNAME` / `TRIM_APPVER`；缺失时（开发调试）回退到 `ETP_FNOS_*`
  环境变量或 `~/.local/share/easytier-pro-fnos` 下的目录树。
- 目录约定：state、secrets、runtime、logs、run 均置于 `$TRIM_PKGVAR` 下
  （升级保留，卸载删除）。
- machine-id：读 `/etc/machine-id`（取首行，空则视为读不到）；读不到则生成
  随机 UUID 持久化到 `state/machine-id`（复用 synology store 的既有行为）。
- TUN 检查：`/dev/net/tun` 存在性探测，结果上报到 API 供 UI 展示。

## 6. HTTP 服务与鉴权

- 监听 Unix socket `$TRIM_APPDEST/app.sock`（不再监听 TCP 端口）。
- socket 权限 0660，属组设为 fnOS nginx worker 用户组（实现时探测，候选
  www-data / nginx / trim）；若探测失败则 0660 root:root 并在文档记录需要
  手工调整——真机验证项见 §12。
- 中间件：除 `/api/healthz` 外所有 `/api/*` 要求请求头
  `X-Trim-Isadmin: true`（该头由 fnOS 网关在验证 NAS 登录态后注入，客户端
  无法经网关伪造；直连 socket 的本地进程本就有 root 等价能力，残余风险可
  接受并文档化）。
- 路由同时挂 `/api/*` 与 `/app/easytier-pro/api/*` 两种前缀（fnOS 网关是否
  剥离前缀属实现时验证项，双挂可兼容两种行为）；静态 UI 同理。
- API 面与 synology 版保持一致：`GET /api/status`、
  `POST /api/auth/device/start|poll`、`POST /api/workspaces/{id}/activate`、
  `POST /api/connect-token`、`GET /api/networks`、
  `POST /api/networks/{id}/join|leave`、`POST /api/runtime/download`、
  `GET /api/logs`、`GET /api/version` 等。token 等密钥永不出现在响应中。

## 7. Daemon 进程模型

照搬 synology 双进程模型：

- `supervise`：由 `cmd/main start` 经 `setsid` 拉起，负责 respawn
  （3600s 窗口 / 5s 延迟 / 5 次上限），pid 写入 `$TRIM_PKGVAR/run/daemon.pid`。
- `serve`：supervise 的子进程，托管 HTTP 服务与 core 生命周期。
- `version`：打印注入的版本号。

## 8. UI 适配（monorepo 单份源码）

- `ui/` 保留一份源码，平台差异收敛到构建期注入的 `src/platform/{dsm,fnos}.ts`
  配置模块：vite `base`（dsm `./`，fnos `/app/easytier-pro/`）、鉴权头
  （dsm 注入 `X-Syno-Token`，fnos 无）、401/403 错误码（`dsm_auth_*` /
  `fnos_auth_*`）、重登链接（dsm `/webman/index.cgi`，fnos `/`）、品牌文案。
- Vite 构建两产物：`npm run build:dsm` → `ui/dist-dsm/`，
  `npm run build:fnos` → `ui/dist-fnos/`，均入库；`ui/embed_dsm.go` /
  `ui/embed_fnos.go` 以 build tag（`dsm` / `fnos`）各自内嵌。
- fnOS 桌面入口经 `fpk/app/ui/config` 注册：

```json
{ ".url": { "easytier-pro.main": {
    "title": "EasyTier Pro",
    "icon": "images/icon_{0}.png",
    "type": "iframe",
    "gatewayPrefix": "/app/easytier-pro",
    "gatewaySocket": "app.sock",
    "url": "/app/easytier-pro/",
    "allUsers": false } } }
```

`fpk/manifest` 中 `desktop_applaunchname=easytier-pro.main`。

## 9. 打包与生命周期

### manifest（INI）

```
appname=easytier-pro
version=0.1.0
display_name=EasyTier Pro
desc=...
source=thirdparty
platform=x86            # arm64 包为 arm
maintainer / maintainer_url / distributor / distributor_url
os_min_version=1.2.0401  # 统一网关身份头的最低版本要求
ctl_stop=true
desktop_uidir=ui
desktop_applaunchname=easytier-pro.main
changelog=...
checksum=<build 时写入 app.tgz 的 md5>
```

### config/privilege 与 config/resource

`privilege`: `{"defaults": {"run-as": "root"}}`。
`resource`: 空声明 `{}`（不申请 data-share / api-scope）。

### cmd/ 脚本（全部幂等，bash）

- `install_init`：检查磁盘空间（≥200MB）；`modprobe tun`（已加载则跳过）；
  `/dev/net/tun` 缺失则 `mknod /dev/net/tun c 10 200`。
- `install_callback`：创建 `$TRIM_PKGVAR/{state,runtime,logs,run,cache}`。
- `main start|stop|status`：setsid 启动 supervise；stop 先 TERM 后 KILL；
  status 经 pid 文件 + `/proc/<pid>/cmdline` 校验，退出码 0/3/1。
- `upgrade_init`：停服务。`upgrade_callback`：保留 state/secrets，启动服务。
- `uninstall_init`：停服务。`uninstall_callback`：`TRIM_APP_STATUS` 非
  UPGRADE 时删除 `$TRIM_PKGVAR` 下 state/runtime/logs/run/cache。
- 用户可见错误写入 `$TRIM_TEMP_LOGFILE` 后以非零退出。

### fpk 结构与构建

`build-fpk.sh` 流程：`npm run build`（一次）→ 按 arch 执行
`CGO_ENABLED=0 go build` → 组装 `app/`（daemon 二进制、ui/ 入口、icons）
→ `tar czf app.tgz`、md5 写入 manifest → 外层 `tar czf
dist/easytier-pro-<ver>-<x86_64|arm64>.fpk`。优先调用官方 `fnpack build`
（本机有 fnpack 时），否则手工 tar（社区验证可行）。`check-fpk.sh` 解包
校验必需文件与 JSON 合法性。

## 10. 运行时下载（照搬 synology internal/runtime）

- 每 30 分钟轮询 `GET {console}/api/v1/releases/latest`，取 `stable.version`
  （严禁用 GitHub/Gitee 的 latest）。
- 下载 `easytier-linux-<arch>-<version>.zip`，Gitee 优先、GitHub 兜底；
  Console 提供 artifact 元数据时校验 size/sha256。
- 事务式更新：下载到临时目录 → 校验 → 原子切换 → 失败回滚；中断恢复。

## 11. 错误处理与日志

- daemon 日志写 `$TRIM_PKGVAR/logs/daemon.log`（带滚动上限），
  `GET /api/logs` 供 UI 展示。
- core stdout/stderr 丢弃（防 token 泄漏）；core 崩溃由 supervise 重启策略
  覆盖。
- 生命周期脚本的用户可见错误写 `$TRIM_TEMP_LOGFILE`。

## 12. 测试策略与交付门槛

- Go 单测：synology 仓 internal/* 全部测试保持绿色；新增 fnosenv 测试。
- `scripts/test-lifecycle.sh`：在本机用伪造的 `TRIM_*` 环境完整跑
  install→start→status→stop→upgrade→uninstall。
- `scripts/check-fpk.sh`：静态校验。
- **交付门槛（用户硬性要求）：必须在真实 fnOS 上完成完整新用户 E2E 流程
  后才可交付。** E2E 环境为 PVE VM 143（fnOS，10.147.223.159，admin 账号
  e2e / E2e-test-2026!，SSH 已开启）。E2E 覆盖：
  1. 手动安装 fpk（应用中心 → 手动安装，或 appcenter-cli install-fpk）。
  2. 桌面图标出现，点击打开 iframe UI，经统一网关正常加载。
  3. 新用户完整流程：device flow 登录 Console → 激活工作区 → 触发
     easytier-core 下载 → 加入网络 → 查看节点状态与日志。
  4. 停止/启动（应用中心或 appcenter-cli），状态正确恢复。
  5. 升级（安装同版本或新版本 fpk），state/secrets 保留。
  6. 卸载，state/runtime/logs/run/cache 清理。
  7. 证据归档：docs/e2e-20260916-fnos/README.md + 截图 + 命令输出。
- 真机验证项（E2E 中确认）：
  1. 统一网关是否剥离 `gatewayPrefix` 前缀（双挂路由已兜底，验证后收敛）。
  2. fnOS nginx worker 用户/组，用于 socket 属组设置。
  3. `/dev/net/tun` 在目标 fnOS 版本的实际状态。
  4. fnpack 对空 `wizard/` 目录的校验要求。
  5. fpk 手动安装全流程（应用中心 → 手动安装）。

## 13. 发布

- `dist/` 存放构建产物；git tag `v<version>` 触发发布流程（GitHub/Gitee
  Releases 上传 spk 与 fpk，monorepo 一次发双包）。
- 上架 fnOS 应用商店需走官方微信群提审流程，属运营事项，不在本仓库范围。

## 14. 非目标（Non-goals）

- 不做 fnOS 特有的深度集成（@trimjs SDK、文件选择器、共享目录授权）。
- 不做 Docker 型包。
- 不实现 fnOS 应用商店上架自动化。
