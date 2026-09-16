# EasyTier Pro fnOS 应用实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把已验证的 Synology 版 EasyTier Pro 客户端移植为 fnOS（飞牛 NAS）原生 .fpk 应用，功能对齐 Synology 版全量功能。

**Architecture:** Go 1.24 零依赖 daemon（supervise/serve 双进程）+ 内嵌 Vue3 UI，经 fnOS 统一网关（Unix socket + X-Trim-* 身份头）接入 fnOS 桌面；easytier-core 由 Console 发布 API 定版、运行时下载；root 权限运行，无 no_tun 降级。

**Tech Stack:** Go 1.24（纯 stdlib）、Vue 3.5 + naive-ui + Vite 7 + TypeScript、bash 生命周期脚本、tar.gz 格式 .fpk。

**设计文档：** `docs/superpowers/specs/2026-09-16-easytier-pro-fnos-design.md`（权威，冲突时以设计文档为准并回改计划）

**移植源：** `/data/project/easytier-pro-synology`（HEAD `fb947ae`）。本计划大量步骤为"从源仓库复制后做有名有姓的最小改动"。

## Global Constraints

- 模块路径 `github.com/EasyTier-Pro/easytier-pro-fnos`，`go 1.24`，**零外部依赖**（无 go.sum）。
- daemon 二进制名 `easytier-pro-fnos`；入口 `cmd/easytier-pro-fnos/main.go`。
- 初始版本 `0.1.0`，单一事实来源为 `fpk/manifest` 的 `version=` 行，构建时 `-X main.buildVersion` 注入。
- 错误码：`fnos_auth_required` / `fnos_auth_forbidden`（替代源仓库的 `dsm_auth_*`），中文错误文案。
- 用户可见文案中文、代码注释英文（源仓库惯例）。
- 网关前缀 `/app/easytier-pro`，Unix socket `$TRIM_APPDEST/app.sock`，UI vite base `/app/easytier-pro/`。
- core 启动契约：`ET_RPC_PORTAL=0.0.0.0:15888`、`ET_RPC_PORTAL_WHITELIST=127.0.0.0/8,::1/128`、`ET_CONSOLE_LOG_LEVEL=off`、`easytier-core --secure-mode=true`，core stdout/stderr 丢弃。
- `run-as: root`；平台仅 x86_64（manifest `platform=x86`）与 arm64（`platform=arm`）两个包。
- 默认 Console `https://api.console.easytier.net`；core 版本永远取 Console `stable.version`。
- 生命周期脚本幂等；用户可见错误写 `$TRIM_TEMP_LOGFILE` 并非零退出。
- `ui/dist` 构建产物入库；`dist/`、`.dev/`、`node_modules` 进 .gitignore。
- git author：`KKRainbow <5665404+KKRainbow@users.noreply.github.com>`（仓库级 config 已设好）；commit message 为 Conventional-Commits 风格、英文、textwidth 72。
- **回执与泄露纪律：** 迁移代码时 `sed` 只做点名替换，替换后必须 `grep -RIn -e 'easytier-pro-dsm' -e 'dsm' -e 'DSM' -e 'synology' -e 'Synology' -e 'SYNOPKG' -e '群晖' -e 'X-Syno'` 全仓回执；`check-fpk.sh` 与 CI 必须含防泄露检查。
- 每个任务结束提交一次 commit（消息见各任务最后一步）。

---

### Task 1: 仓库骨架与 Go 骨架（main + config 移植）

**Files:**
- Create: `go.mod`、`cmd/easytier-pro-fnos/main.go`
- Create: `internal/config/{paths.go,store.go,atomic.go,validate.go,log.go,config_test.go}`
- Create: `.gitignore`、`LICENSE`（Apache-2.0，复制自源仓库）、`README.md`（中文，先写简介+开发指引骨架）

**Interfaces:**
- Produces（后续任务依赖）:
  - `config.Paths{PkgDest, PkgVar string}` 及全部派生方法：`RuntimeDir/StateDir/SecretsDir/LogsDir/RunDir/OperationsDir/ArchiveCacheDir/SettingsFile/MachineIDFile/BootstrapTokenFile/SessionFile/DeviceAuthFile/DownloadStatusFile/ConnectionTransactionFile/UpdateTransactionFile/DaemonLogFile/PidFile/CorePIDFile/CoreBinary/CLIbinary/DaemonBinary/UIDir/EnsureDirs()`（`CLIbinary` 沿用源仓库拼写，runtime 包有 15+ 处引用，不得改名）
  - `config.ResolvePaths() (Paths, error)`：读 `TRIM_APPDEST`/`TRIM_PKGVAR`，`DevRoot()` 非空时回退 `<devroot>/target|var`（开发用，对应源仓库 `ETP_DEV_ROOT`）
  - `config.DevRoot() string` / `config.DevMode() bool`
  - `config.DefaultConsoleURL`、`config.Settings{Enabled, ConsoleURL, AllowInsecureConsole, ConfigServer, ActiveWorkspaceID}`、`config.Store`（`NewStore/Paths/Settings/SaveSettings/SaveBootstrapToken/ReadBootstrapToken/RestoreBootstrapToken/HasBootstrapToken/RemoveBootstrapToken/MachineID/RandomUUID`）
  - `config.Logger`（`NewLogger(path, verbose)/Printf/Errorf/Close`，5MB 滚动）
  - `config.AtomicWrite/SyncDir/ReadSecret`、`config.ValidUUID/ValidSecret/ValidConsoleURL/ValidConfigServer/NormalizeConsoleURL/NormalizeConfigServer`
  - `main` 子命令：`supervise`（默认）/`serve`/`version`；`var buildVersion = "dev"`

- [ ] **Step 1: 初始化骨架文件**

```bash
cd /data/project/easytier-pro-fnos
cp /data/project/easytier-pro-synology/LICENSE .
printf 'module github.com/EasyTier-Pro/easytier-pro-fnos\n\ngo 1.24\n' > go.mod
printf '/dist/\n/.dev/\nui/node_modules/\n' > .gitignore
mkdir -p cmd/easytier-pro-fnos internal/{config,console,fnosenv,httpserver,runtime} fpk scripts tests ui
```

README.md 先写最小中文骨架（项目简介一句话 + "开发中" 标注），后续任务补强。

- [ ] **Step 2: 移植 internal/config（复制 → sed → 回执）**

```bash
cd /data/project/easytier-pro-fnos
cp /data/project/easytier-pro-synology/internal/config/*.go internal/config/
sed -i 's/SYNOPKG_PKGDEST/TRIM_APPDEST/g; s/SYNOPKG_PKGVAR/TRIM_PKGVAR/g; s/easytier-pro-dsm/easytier-pro-fnos/g; s/easytier-pro-dsm\\.go/easytier-pro-fnos.go/g' internal/config/*.go
grep -RIn -e 'SYNOPKG' -e 'dsm' -e 'DSM' -e 'ynolog' internal/config/ || echo CLEAN
```

预期回执 CLEAN（注释里若出现需手工改为 fnOS 语境）。逐文件 Read 确认：
- `paths.go`：`ResolvePaths` 现在读 TRIM_*；`DaemonBinary()` 指向 `PkgDest/bin/easytier-pro-fnos`；错误消息改为 `"TRIM_APPDEST and TRIM_PKGVAR must be set"`。
- `log.go` 顶部注释若提 DSM 改为 fnOS 语境（英文注释）。

- [ ] **Step 3: 移植 cmd/main.go**

```bash
cp /data/project/easytier-pro-synology/cmd/easytier-pro-dsm/main.go cmd/easytier-pro-fnos/main.go
```

然后逐处 Edit（不要用 sed 一把梭，这个文件需要语义改动）：
1. `easytier-pro-dsm` 全部字样 → `easytier-pro-fnos`。
2. 删 `const listenAddress = "127.0.0.1:15890"` 与 `listenAddr()`；新增 socket 解析：

```go
const socketName = "app.sock"

// controlSocket returns the Unix socket path the fnOS gateway proxies to.
func controlSocket(paths config.Paths) string {
	if override := os.Getenv("ETP_DEV_LISTEN"); config.DevMode() && override != "" {
		return override
	}
	return filepath.Join(paths.PkgDest, socketName)
}
```

3. `dsmenv.New(logger)` → `fnosenv.New(logger)`（import 同步改；fnosenv 在 Task 5 才存在，本任务先让 main.go 处于"结构正确但编译不过"的中间态，Step 5 用临时 stub 恢复编译）。
4. serve 的 listener 换成 Unix socket（删除旧 socket 残留文件→Listen→chmod→serve）：

```go
	sockPath := controlSocket(paths)
	_ = os.Remove(sockPath)
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", sockPath, err)
	}
	defer listener.Close()
	// The fnOS gateway (nginx worker) must be able to connect.
	if err := os.Chmod(sockPath, 0o666); err != nil {
		return fmt.Errorf("chmod %s: %w", sockPath, err)
	}
	...
	serveErr := server.Serve(listener)
```

5. `runSupervise` 内部 `exec.Command(paths.DaemonBinary(), "serve")` 等逻辑原样保留。

- [ ] **Step 4: 最小 fnosenv stub + ui stub（仅为编译通过）**

`internal/fnosenv/fnosenv.go`：

```go
// Package fnosenv integrates with the fnOS gateway identity headers.
package fnosenv

import (
	"context"
	"net/http"

	"github.com/EasyTier-Pro/easytier-pro-fnos/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-fnos/internal/config"
)

// Authenticator trusts the identity headers injected by the fnOS gateway.
type Authenticator struct {
	bypass bool
	log    *config.Logger
}

// New enables the dev bypass only in dev mode with an explicit opt-in.
func New(log *config.Logger) *Authenticator {
	return &Authenticator{
		bypass: config.DevMode() && os.Getenv("ETP_DEV_NO_FNOS_AUTH") == "1",
		log:    log,
	}
}

// Bypassed reports whether header checks are disabled (dev only).
func (a *Authenticator) Bypassed() bool { return a.bypass }

// Authenticate rejects requests that did not come through the gateway as an admin.
func (a *Authenticator) Authenticate(ctx context.Context, r *http.Request) (string, *apperr.Error) {
	if a.bypass {
		return "dev", nil
	}
	if r.Header.Get("X-Trim-Isadmin") != "true" {
		return "", apperr.New(apperr.CodeFnOSAuthRequired)
	}
	name := r.Header.Get("X-Trim-Username")
	if name == "" {
		name = "admin"
	}
	return name, nil
}
```

（需要 `os` import。apperr 在 Task 2 才正式落地，本步同时建最小 `internal/apperr/apperr.go`：只含 `Error` 类型、`New/WithMessage`、`CodeFnOSAuthRequired`/`CodeFnOSAuthForbidden` 两个码与中文消息 `"请先登录 fnOS。"` / `"只有 fnOS 管理员可以使用该功能。"`；Task 2 再补全 48 个码。）

`ui/embed.go` + 最小 dist（Task 6 再换真 UI）：

```go
// Package ui embeds the built web assets.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS holds the interface assets rooted at the top level.
var FS fs.FS = mustSub()

func mustSub() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
```

`ui/dist/index.html`：`<!doctype html><title>EasyTier Pro</title>placeholder`。

- [ ] **Step 5: 暂时移除 main.go 对尚不存在包的引用**

`manager/client/httpserver` 在 Task 3/4/5 才落地。本任务把 `runServe` 主体暂时收敛为：解析路径、EnsureDirs、Logger、建 `http.Server{Handler: http.NotFoundHandler()}` + Unix socket 监听 + 信号处理。用注释标记 `// wiring completed in later tasks`。保证 `go build ./...` 通过。

- [ ] **Step 6: 跑 config 测试**

```bash
cd /data/project/easytier-pro-fnos && go vet ./... && go test ./internal/config/
```

预期：源仓库 8 个测试（改名后）全 PASS。若有测试引用 SYNOPKG 字样，同步改名。

- [ ] **Step 7: Commit**

```bash
git add -A && git commit -m "feat: scaffold repo with config package and daemon skeleton"
```

---

### Task 2: 移植 apperr（错误码改名）

**Files:**
- Modify: `internal/apperr/apperr.go`（用源仓库完整版替换 Task 1 的最小 stub）

**Interfaces:**
- Produces: `apperr.Error{Code, Message}`、`New/WithMessage`、全部 47 个错误码；其中 `CodeFnOSAuthRequired = "fnos_auth_required"`、`CodeFnOSAuthForbidden = "fnos_auth_forbidden"`（中文消息见 Task 1 stub）。源仓库的 `CodeDSMAuthRequired/CodeDSMAuthForbidden` **不得存在**。

- [ ] **Step 1: 复制全量 apperr 并改名**

```bash
cp /data/project/easytier-pro-synology/internal/apperr/apperr.go internal/apperr/apperr.go
```

逐处 Edit：`CodeDSMAuthRequired` → `CodeFnOSAuthRequired`（值 `"fnos_auth_required"`，消息 `"请先登录 fnOS。"`）；`CodeDSMAuthForbidden` → `CodeFnOSAuthForbidden`（值 `"fnos_auth_forbidden"`，消息 `"只有 fnOS 管理员可以使用该功能。"`）。注释中的 DSM 语境改 fnOS。

- [ ] **Step 2: 回执 + 编译**

```bash
grep -RIn -e 'DSM' -e 'dsm_' internal/apperr/ || echo CLEAN
go build ./... && go test ./internal/...
```

预期 CLEAN、编译通过、config 测试仍绿。

- [ ] **Step 3: Commit**

```bash
git add -A && git commit -m "feat(apperr): port error catalogue with fnOS auth codes"
```

---

### Task 3: 移植 console 包（平台中立，仅品牌串）

**Files:**
- Create: `internal/console/{client.go,device.go,enroll.go,networks.go,relay.go,release.go,types.go,workspace.go}`
- Create: `internal/console/{console_test.go,relay_test.go,workspace_test.go}`

**Interfaces:**
- Produces（httpserver/runtime 依赖）: `console.Client`（`New(store, log)`）及方法 `LoggedIn/ClearSession/SessionReady/RefreshSession/AuthStatus/AuthStart/AuthPoll/AuthMe/Logout/LatestRelease/EnrollmentOptions/Activate/EnrollmentKeySecret/Networks/NetworkNodes/NetworkJoin/NetworkLeave/MachineState/DeclareDeviceDefaults/SetNodeMode`；类型 `AuthStatusResult/DeviceAuthStartResult/DeviceAuthPollResult/Release/VersionInfo/Artifact/ActivateResult/EnrollmentOptions/NetworksResult/NodesResult/LeaveResult/NodeMode{NoTun, DisableBindDevice}/MachineState`；常量 `ModeAuto/ModeLocal/ModeRecover/ModeExisting/ModeShared/ModeDedicated`、`ValidEnrollmentMode`；`Release.ConfigServerURL()`。

- [ ] **Step 1: 复制 + sed + 回执**

```bash
cp /data/project/easytier-pro-synology/internal/console/*.go internal/console/
sed -i 's/Synology NAS/fnOS NAS/g; s/Synology %s-%s/fnOS %s-%s/g' internal/console/*.go
grep -RIn -e 'Synology' -e 'synology' -e 'DSM' -e 'dsm' internal/console/ || echo CLEAN
```

若有注释残留（如 "Synology" 出现在说明文字）手工改 fnOS 语境（英文注释）。

- [ ] **Step 2: 编译 + 测试**

```bash
go vet ./... && go test ./internal/console/
```

预期 16 个测试全 PASS（console_test 6 + relay_test 5 + workspace_test 5）。

- [ ] **Step 3: Commit**

```bash
git add -A && git commit -m "feat(console): port console API client unchanged"
```

---

### Task 4: 移植 runtime 包（平台中立）

**Files:**
- Create: `internal/runtime/{archivecache.go,connection.go,core.go,download.go,instancewatch.go,manager.go,operations.go,relay.go,releasewatch.go,rpcportal.go,state.go,tun.go}`
- Create: `internal/runtime/{archivecache_test.go,instancewatch_test.go,relay_test.go,releasewatch_test.go,runtime_test.go,tun_test.go}`（以源仓库实际文件清单为准）

**Interfaces:**
- Produces（main/httpserver 依赖）: `runtime.Manager`（`NewManager(paths, store, cli, log)`）及 `Start(ctx)/Shutdown()/Status()/LocalSummary(ctx)/Logs(n)/Settings()/ApplySettings(ctx, Settings)/ServiceAction(ctx, action)/DownloadStart(version)/DownloadStatus()/Activate(ctx, ws, mode, keyID)/ConnectToken(ctx, token, configServer)/Disconnect(ctx)/NetworkJoin/NetworkLeave(ctx, networkID)/OperationStatus(id)/NodeMode()/SyncRelayMode(ctx)/ModeSynced()`；类型 `Status{...}`、`Operation{...}`、`DownloadStatus{...}`。

- [ ] **Step 1: 复制 + sed + 回执**

```bash
cp /data/project/easytier-pro-synology/internal/runtime/*.go internal/runtime/
sed -i 's/Synology NAS/fnOS NAS/g' internal/runtime/*.go
grep -RIn -e 'Synology' -e 'synology' -e 'DSM' -e 'dsm' internal/runtime/ || echo CLEAN
```

注释里 DSM/DSM7 的动机说明（tun.go 等）可保留历史背景但须把平台名表述改准确；`manager.go:169` 的 hostname fallback 已被 sed 覆盖，Read 确认。

- [ ] **Step 2: 确认 RPC portal 常量不被误改**

`internal/runtime/rpcportal.go` 应保持：`rpcPortalListen = "0.0.0.0:15888"`、`rpcPortalAddress = "127.0.0.1:15888"`、`rpcPortalWhitelist = "127.0.0.0/8,::1/128"`。Read 该文件确认。

- [ ] **Step 3: 编译 + 测试**

```bash
go vet ./... && go test ./internal/runtime/
```

预期约 52 个测试全 PASS。`tun.go` 的 root 探测（`os.Geteuid()==0`）在 root 运行的 CI/容器里会让部分用例走 root 分支——源仓库测试已处理该情况，保持原样。

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat(runtime): port core supervisor and transactional downloader"
```

---

### Task 5: fnosenv 正式实现 + httpserver 移植 + main 接线

**Files:**
- Modify: `internal/fnosenv/fnosenv.go`（Task 1 stub → 完整实现：log 使用、username 校验）
- Create: `internal/fnosenv/fnosenv_test.go`
- Create: `internal/httpserver/{server.go,handlers.go,server_test.go}`（复制自源仓库后改）
- Modify: `cmd/easytier-pro-fnos/main.go`（恢复 runServe 完整接线）

**Interfaces:**
- Consumes: 全部前序任务的 Produce。
- Produces:
  - `fnosenv.Authenticator`：`New(log)/Bypassed()/Authenticate(ctx, *http.Request) (string, *apperr.Error)`（与源仓库 dsmenv 形状一致）
  - `httpserver.New(manager, client, auth, log, static fs.FS) *Server`、`Server.Handler() http.Handler`
  - 网关前缀常量 `gatewayPrefix = "/app/easytier-pro"`（httpserver 内部）

- [ ] **Step 1: fnosenv 完整实现（TDD）**

先写 `fnosenv_test.go`：

```go
func TestRejectsMissingHeader(t *testing.T)      // 无 X-Trim-Isadmin → CodeFnOSAuthRequired
func TestRejectsNonAdmin(t *testing.T)           // X-Trim-Isadmin: false → CodeFnOSAuthForbidden
func TestAcceptsAdmin(t *testing.T)              // true → identity = X-Trim-Username
func TestUsernameFallback(t *testing.T)          // isadmin=true 且无 username → "admin"
func TestUsernameSanitized(t *testing.T)         // 含 \r\n 或 >64 字符 → 截断/拒绝
func TestDevBypass(t *testing.T)                 // ETP_DEV_ROOT + ETP_DEV_NO_FNOS_AUTH=1 → Bypassed()==true
func TestNoBypassOutsideDev(t *testing.T)        // 仅 ETP_DEV_NO_FNOS_AUTH=1 → 不 bypass
```

实现要点（相对 Task 1 stub 的增量）：
- `Authenticate` 逻辑：

```go
	if a.bypass {
		return "dev", nil
	}
	switch r.Header.Get("X-Trim-Isadmin") {
	case "true":
		name := sanitizeUsername(r.Header.Get("X-Trim-Username"))
		if name == "" {
			name = "admin"
		}
		return name, nil
	case "":
		return "", apperr.New(apperr.CodeFnOSAuthRequired)
	default:
		return "", apperr.New(apperr.CodeFnOSAuthForbidden)
	}
```

- `sanitizeUsername`：去掉控制字符、限长 64、只取第一个值（`r.Header.Get` 已取第一个）。

```bash
go test ./internal/fnosenv/   # 预期 7 个测试 PASS
```

- [ ] **Step 2: 移植 httpserver（复制 → 语义改动）**

```bash
cp /data/project/easytier-pro-synology/internal/httpserver/*.go internal/httpserver/
```

逐处 Edit（`server.go`）：
1. import `dsmenv` → `fnosenv`，字段类型同步。
2. `requireDSMSession` → `requireFnosSession`；内部 `Authenticate` 调用的错误码映射：`CodeFnOSAuthRequired` → 401、`CodeFnOSAuthForbidden` → 403（结构照抄源仓库）。
3. `checkSameOrigin` 原样保留。
4. 网关前缀双挂（`Handler()` 末尾）：

```go
	root := http.NewServeMux()
	root.Handle("/api/", s.requireFnosSession(api))
	root.Handle("/", s.staticHandler())

	// The fnOS gateway may or may not strip the gateway prefix before
	// proxying; accept both forms until verified on real hardware.
	mux := http.NewServeMux()
	mux.Handle("/", root)
	mux.Handle("/app/easytier-pro/", http.StripPrefix("/app/easytier-pro", root))
	return mux
```

5. 日志/注释中 DSM 字样改 fnOS。

`handlers.go`、`server_test.go` 复制后只改字样（dsm→fnos、`dsm_auth_*`→`fnos_auth_*`）。`server_test.go` 的 4 个 origin/marker 测试应与鉴权无关、直接通过；若有 dsmenv 依赖则改用 dev-bypass 的 `fnosenv.New`。

- [ ] **Step 3: main.go 完整接线**

把 Task 1 临时收敛的 `runServe` 恢复为源仓库结构（对照源文件 `cmd/easytier-pro-dsm/main.go:133-188` 抄回）：

```go
	store := config.NewStore(paths)
	client := console.New(store, logger)
	manager := runtime.NewManager(paths, store, client, logger)
	...
	if err := manager.Start(ctx); err != nil { ... }
	auth := fnosenv.New(logger)
	if auth.Bypassed() { logger.Printf(...) }
	server := &http.Server{
		Handler:           httpserver.New(manager, client, auth, logger, ui.FS).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
```

（`Addr` 字段删除——listener 由 Step Task1-4 的 Unix socket 代码提供；保留 `manager.Shutdown()` → `server.Shutdown` 10s 的顺序。）

- [ ] **Step 4: 全量编译 + 测试 + 冒烟**

```bash
go vet ./... && go test ./...
ETP_DEV_ROOT=$(mktemp -d) ETP_DEV_NO_FNOS_AUTH=1 go run ./cmd/easytier-pro-fnos supervise &
sleep 1; curl --unix-socket $ETP_DEV_ROOT/target/app.sock http://localhost/api/status | jq .
kill %1
```

预期：`{"ok":true,...}` 形态响应（status 字段可空），进程退出码 0。

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: port HTTP server with fnOS gateway auth and wire daemon"
```

---

### Task 6: UI 移植与适配

**Files:**
- Create: `ui/` 全量（复制源仓库 `ui/`，排除 `node_modules`；保留已入库的 `dist/` 但本任务会重建覆盖）
- Modify: `ui/package.json`（name → `easytier-pro-fnos-ui`）
- Modify: `ui/vite.config.ts`（base → `'/app/easytier-pro/'`，注释同步改）
- Modify: `ui/src/api/client.ts`、`ui/src/api/errors.ts`、`ui/src/views/*.vue`、`ui/src/components/{TunNotice,DeviceLoginDialog}.vue`、`ui/src/**/*.test.ts`（详见步骤）
- Modify: `ui/embed.go` 注释字样

**Interfaces:**
- Consumes: Task 5 的 API 面（路由与源仓库 1:1，错误码已改名）。
- Produces: `ui.FS`（编译期内嵌 `ui/dist`）；UI 所有 API 调用走相对路径 `api/...`，写操作带 `X-Easytier-Request: 1` 头；401/403 映射 `fnos_auth_required`/`fnos_auth_forbidden`。

- [ ] **Step 1: 复制 UI 源码**

```bash
cd /data/project/easytier-pro-fnos
rsync -a --exclude node_modules --exclude dist /data/project/easytier-pro-synology/ui/ ui/
sed -i 's/easytier-pro-dsm-ui/easytier-pro-fnos-ui/' ui/package.json
```

- [ ] **Step 2: vite.config.ts**

`base: './'` → `base: '/app/easytier-pro/'`，注释改为 "served under the fnOS gateway prefix /app/easytier-pro/"。

- [ ] **Step 3: api/client.ts 改动**

对照源文件 `ui/src/api/client.ts`：
1. 删除整个 `synoToken()` 函数（源文件 32-58 行）及其调用点（69 行附近的 `headers['X-Syno-Token'] = ...`）。
2. 保留 `X-Easytier-Request: 1` 头与 `credentials: 'same-origin'`。
3. 87-89 行的 401/403 映射：`dsm_auth_required` → `fnos_auth_required`、`dsm_auth_forbidden` → `fnos_auth_forbidden`。
4. 顶部 DSM 注释改为 fnOS 网关语境。

- [ ] **Step 4: api/errors.ts 改动**

15-16 行的码与消息改为 `fnos_auth_required: '请先登录 fnOS。'`、`fnos_auth_forbidden: '只有 fnOS 管理员可以使用该功能。'`；`needsRelogin()`（65-68 行）检查的新码同步。

- [ ] **Step 5: 视图与组件文案**

逐处 Edit（先 grep 定位）：

```bash
grep -RIn -e 'DSM' -e 'dsm' -e 'Synology' -e 'synology' -e '群晖' -e 'webman' -e 'X-Syno' ui/src/
```

预期命中（行号以源仓库为准，移植后可能漂移）：
- `views/OverviewView.vue`（约 472-498）："DSM 会话已过期" 面板与重新登录按钮——文案改 "fnOS 会话"，重登链接 `/webman/index.cgi` → `/`（fnOS 桌面首页；iframe 内跳转顶层用 `target="_top"`，三个视图一致）。
- `views/NetworksView.vue`（约 146-191）：同上重登链接处理；DSM 字样改 fnOS。
- `views/SettingsView.vue`（约 114-184）："群晖开机后自动重新接入…" → "fnOS 开机后自动重新接入…"；"安装目录由 DSM 统一管理" → "安装目录由 fnOS 应用中心统一管理"；DSM 控制面板防火墙提示删除或改通用表述（fnOS 无对应面板）。
- `views/LogsView.vue`（约 61, 120）：DSM 字样改 fnOS。
- `components/TunNotice.vue`：整体是 DSM7 非 root 降级说明。fnOS root 运行无此场景，组件保留但标题/正文改为 "/dev/net/tun 不可用" 的通用提示（说明需要 tun 内核模块）；daemon 的 `TUNCapable` 字段在 root 下恒 true，该组件实际不会弹出，改动从简。
- `components/DeviceLoginDialog.vue`（约 60）："请重新登录 DSM 后再试。" → "请重新登录 fnOS 后再试。"
- `utils/format.test.ts`、`views/OverviewView.test.ts`、`api/client.test.ts`：fixture 里的 `synology-nas`、`dsm_auth_*` 期望同步改。

- [ ] **Step 6: 构建 + 测试 + 回执**

```bash
cd ui && npm ci --no-audit --no-fund && npm run test && npm run build && cd ..
grep -RIn -e 'DSM' -e 'dsm' -e 'Synology' -e 'synology' -e '群晖' -e 'webman' -e 'X-Syno' ui/src/ ui/dist/ || echo CLEAN
go build ./...   # embed 新 dist 后仍编译通过
```

预期：vitest 全绿、`ui/dist/index.html` 存在、回执 CLEAN（dist 里的哈希资源名不含这些词）。

- [ ] **Step 7: Commit**

```bash
git add -A && git commit -m "feat(ui): port web interface to fnOS gateway context"
```

---

### Task 7: fpk 打包层（manifest/cmd/config/入口/图标）

**Files:**
- Create: `fpk/manifest`
- Create: `fpk/config/{privilege,resource}`
- Create: `fpk/cmd/{install_init,install_callback,main,upgrade_init,upgrade_callback,uninstall_init,uninstall_callback}`（755）
- Create: `fpk/app/ui/config`、`fpk/app/ui/images/`（图标集）
- Create: `fpk/ICON.PNG`、`fpk/ICON_256.PNG`
- Create: `fpk/wizard/.gitkeep`（空目录占位）

**Interfaces:**
- Consumes: daemon 二进制名 `easytier-pro-fnos`；socket 名 `app.sock`（Task 1 常量）；网关前缀 `/app/easytier-pro`。
- Produces: fpk 源树布局（build-fpk.sh 的输入）：
  - 安装后 daemon 位于 `$TRIM_APPDEST/bin/easytier-pro-fnos`（与 `config.Paths.DaemonBinary()` 一致）
  - `cmd/main` 管理 `supervise` 进程，pid 文件 `$TRIM_PKGVAR/run/daemon.pid`

- [ ] **Step 1: manifest**

```ini
appname=easytier-pro
version=0.1.0
display_name=EasyTier Pro
desc=把本机接入 EasyTier Console 的虚拟网络，提供组网状态、网络管理与日志查看。
source=thirdparty
platform=x86
maintainer=EasyTier Project
maintainer_url=https://github.com/EasyTier-Pro
distributor=EasyTier Project
distributor_url=https://github.com/EasyTier-Pro
os_min_version=1.2.0401
ctl_stop=true
desktop_uidir=ui
desktop_applaunchname=easytier-pro.main
changelog=首个 fnOS 版本，功能对齐 Synology 版。
checksum=
```

（`checksum` 由 build 脚本写入；`platform` 由 build 脚本按架构 sed 为 `x86`/`arm`。）

- [ ] **Step 2: config/privilege 与 config/resource**

`fpk/config/privilege`：

```json
{
	"defaults": {
		"run-as": "root"
	}
}
```

`fpk/config/resource`：`{}`

- [ ] **Step 3: cmd/main（start/stop/status）**

照源仓库 `spk/scripts/start-stop-status` 的模式改写（变量名从 SYNOPKG_* 换 TRIM_*）：

```bash
#!/bin/bash
# EasyTier Pro fnOS service control: start | stop | status
set -u

DAEMON="$TRIM_APPDEST/bin/easytier-pro-fnos"
PIDFILE="$TRIM_PKGVAR/run/daemon.pid"

daemon_pid() {
	[ -f "$PIDFILE" ] || return 1
	pid=$(cat "$PIDFILE" 2>/dev/null) || return 1
	[ -n "$pid" ] && [ -d "/proc/$pid" ] || return 1
	grep -q "easytier-pro-fnos" "/proc/$pid/cmdline" 2>/dev/null || return 1
	printf '%s' "$pid"
}

start_daemon() {
	mkdir -p "$TRIM_PKGVAR/run"
	if pid=$(daemon_pid); then
		exit 0
	fi
	setsid "$DAEMON" supervise </dev/null >>"$TRIM_PKGVAR/logs/boot.log" 2>&1 &
	exit 0
}

stop_daemon() {
	if ! pid=$(daemon_pid); then
		rm -f "$PIDFILE"
		exit 0
	fi
	kill "$pid" 2>/dev/null
	for _ in $(seq 1 50); do
		[ -d "/proc/$pid" ] || break
		sleep 0.2
	done
	if [ -d "/proc/$pid" ]; then
		kill -9 "$pid" 2>/dev/null
	fi
	rm -f "$PIDFILE"
	exit 0
}

case "${1:-}" in
	start) start_daemon ;;
	stop) stop_daemon ;;
	status)
		if daemon_pid >/dev/null; then
			exit 0
		fi
		exit 3
		;;
	*)
		echo "usage: $0 {start|stop|status}" >&2
		exit 1
		;;
esac
```

（日志目录在 install_callback 创建；start 里 `mkdir -p` 兜底保证幂等。）

- [ ] **Step 4: install_init / install_callback**

`fpk/cmd/install_init`：

```bash
#!/bin/bash
# Pre-install checks: disk space and the tun device.
set -u

need_kb=204800
avail_kb=$(df -k "$TRIM_APPDEST_VOL" 2>/dev/null | awk 'NR==2 {print $4}')
if [ -n "$avail_kb" ] && [ "$avail_kb" -lt "$need_kb" ]; then
	echo "磁盘可用空间不足 200MB，无法安装。" >"$TRIM_TEMP_LOGFILE" 2>/dev/null || true
	exit 1
fi

if ! lsmod | grep -q '^tun '; then
	modprobe tun 2>/dev/null || true
fi
if [ ! -c /dev/net/tun ]; then
	mkdir -p /dev/net
	mknod /dev/net/tun c 10 200 2>/dev/null || true
fi
exit 0
```

`fpk/cmd/install_callback`：

```bash
#!/bin/bash
# Post-install: create the package var tree.
set -u
mkdir -p "$TRIM_PKGVAR"/{state,runtime,logs,run,cache}
exit 0
```

- [ ] **Step 5: upgrade / uninstall 脚本**

`fpk/cmd/upgrade_init`：`"$TRIM_APPDEST/../cmd/main" stop` 不可靠——直接内联 stop 逻辑不优雅；约定 cmd 脚本互调走绝对路径 `$TRIM_APPDEST` 的同级 `cmd` 目录。源仓库生命周期脚本是自包含的，这里同样自包含：upgrade_init 内容为"若 pid 存在则停"（复制 main 脚本 stop_daemon 段落，不带 exit 码语义差异）。upgrade_callback：`exit 0`（数据天然保留，应用中心升级不动 $TRIM_PKGVAR）。

`fpk/cmd/uninstall_init`：同 upgrade_init 的停服逻辑。
`fpk/cmd/uninstall_callback`：

```bash
#!/bin/bash
# Post-uninstall: drop state unless this is an upgrade.
set -u
if [ "${TRIM_APP_STATUS:-}" = "UPGRADE" ]; then
	exit 0
fi
cd "$TRIM_PKGVAR" 2>/dev/null && rm -rf state runtime logs run cache
exit 0
```

全部 `chmod 755 fpk/cmd/*`。

- [ ] **Step 6: 桌面入口与图标**

`fpk/app/ui/config`：

```json
{
	".url": {
		"easytier-pro.main": {
			"title": "EasyTier Pro",
			"desc": "把本机接入 EasyTier Console 的虚拟网络",
			"icon": "images/icon_{0}.png",
			"type": "iframe",
			"gatewayPrefix": "/app/easytier-pro",
			"gatewaySocket": "app.sock",
			"url": "/app/easytier-pro/",
			"allUsers": false
		}
	}
}
```

图标：

```bash
cp /data/project/easytier-pro-synology/spk/icons/PACKAGE_ICON.PNG fpk/ICON.PNG
cp /data/project/easytier-pro-synology/spk/icons/PACKAGE_ICON_256.PNG fpk/ICON_256.PNG
mkdir -p fpk/app/ui/images
cp /data/project/easytier-pro-synology/ui/public/images/app_*.png fpk/app/ui/images/
# 重命名 app_N.png → icon_N.png（入口 config 引用的是 icon_{0}.png）
cd fpk/app/ui/images && for f in app_*.png; do mv "$f" "icon_${f#app_}"; done
```

- [ ] **Step 7: 静态自检 + Commit**

```bash
bash -n fpk/cmd/* && jq . fpk/config/privilege fpk/config/resource fpk/app/ui/config >/dev/null
file fpk/ICON.PNG fpk/ICON_256.PNG   # 64x64 / 256x256 PNG
git add -A && git commit -m "feat(fpk): add package manifest, lifecycle scripts and desktop entry"
```

---

### Task 8: 构建/校验/生命周期测试脚本

**Files:**
- Create: `scripts/build-fpk.sh`
- Create: `scripts/check-fpk.sh`
- Create: `scripts/test-lifecycle.sh`（移植源仓库 `tests/smoke-lifecycle.sh` 思路 + 预检，合并为一个脚本）
- Create: `tests/.gitkeep`（不需要则略）

**Interfaces:**
- Consumes: `fpk/manifest` 的 `version=`；Go 构建 `./cmd/easytier-pro-fnos`；UI 构建 `ui/dist`；fpk 源树。
- Produces:
  - `scripts/build-fpk.sh [--arch x86_64|arm64|all] [--version x.y.z] [--out dist]` → `dist/easytier-pro-<arch>-<version>.fpk`
  - `scripts/check-fpk.sh <fpk...>` → 结构校验通过/失败
  - `scripts/test-lifecycle.sh` → 在无 fnOS 的本机模拟完整生命周期

- [ ] **Step 1: build-fpk.sh**

结构对照源仓库 `scripts/build-spk.sh`（POSIX sh, `set -eu`）。核心逻辑：

```sh
#!/bin/sh
# Build easytier-pro .fpk packages for fnOS.
set -eu
# usage: build-fpk.sh [--arch x86_64|arm64|all] [--version X.Y.Z] [--out DIR]

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

# --- arg parse (arch default all, version from fpk/manifest, out default dist) ---
VERSION=${VERSION:-$(sed -n 's/^version=\(.*\)$/\1/p' fpk/manifest)}

build_ui() {
	( cd ui && npm ci --no-audit --no-fund && npm run build )
	[ -f ui/dist/index.html ] || { echo "ui build missing dist/index.html" >&2; exit 1; }
}

# per arch: GOARCH mapping x86_64→amd64, arm64→arm64; manifest platform x86|arm
build_one() {  # $1=arch label
	work=$(mktemp -d)
	trap 'rm -rf "$work"' EXIT
	mkdir -p "$work/app/bin"
	CGO_ENABLED=0 GOOS=linux GOARCH=$GOARCH go build -trimpath \
		-ldflags "-s -w -X main.buildVersion=$VERSION" \
		-o "$work/app/bin/easytier-pro-fnos" ./cmd/easytier-pro-fnos
	cp -r ui/dist/. "$work/app/ui/"          # 静态资源与桌面入口同目录
	cp fpk/app/ui/config "$work/app/ui/config"
	mkdir -p "$work/app/ui/images" && cp fpk/app/ui/images/* "$work/app/ui/images/"
	( cd "$work/app" && tar czf "$work/app.tgz" . )
	sum=$(md5sum "$work/app.tgz" | awk '{print $1}')
	sed -e "s/^version=.*/version=$VERSION/" \
	    -e "s/^platform=.*/platform=$PLATFORM/" \
	    -e "s/^checksum=.*/checksum=$sum/" fpk/manifest >"$work/manifest"
	cp -r fpk/cmd fpk/config fpk/wizard "$work/"
	chmod 755 "$work/cmd/"*
	cp fpk/ICON.PNG fpk/ICON_256.PNG LICENSE "$work/"
	( cd "$work" && tar czf "$OLDPWD/dist/easytier-pro-$1-$VERSION.fpk" \
		manifest app.tgz cmd config wizard ICON.PNG ICON_256.PNG LICENSE )
}
```

注意点：
- daemon 内嵌 UI（go:embed），`app/ui/` 里放静态资源是为 fnOS 桌面入口的 icon 解析与可能的直接访问；`ui/config` 必须在安装后的 `$TRIM_APPDEST/ui/config`（应用中心读取）——与源仓库 spk 的 `dsmuidir` 布局同理。
- 外层 fpk 用 gzip tar（社区验证格式）；若本机存在 `fnpack`，脚本优先 `fnpack build` 并跳过手工 tar（`command -v fnpack` 分支）。
- `trap` 清理每个架构的 work 目录（循环内重新设置）。

- [ ] **Step 2: check-fpk.sh**

对照源仓库 `scripts/check-spk.sh`（依赖 `jq`、`tar -tzf` 列成员）：

```sh
#!/bin/sh
# Static checks for built .fpk packages.
set -eu
# per fpk:
#  1. tar members: manifest app.tgz cmd/main cmd/install_init cmd/install_callback
#     cmd/upgrade_init cmd/upgrade_callback cmd/uninstall_init cmd/uninstall_callback
#     config/privilege config/resource wizard ICON.PNG ICON_256.PNG LICENSE
#  2. manifest keys: appname version display_name source platform os_min_version
#     ctl_stop desktop_uidir desktop_applaunchname checksum（非空且为 32 位 md5）
#  3. platform ∈ {x86, arm} 且与文件名 -x86_64/-arm64 一致
#  4. config/privilege 是合法 JSON 且 defaults.run-as == "root"
#  5. config/resource 是合法 JSON；app.tgz 内 ui/config 合法 JSON 且
#     gatewayPrefix == "/app/easytier-pro"、gatewaySocket == "app.sock"
#  6. 所有 cmd/* 可执行
#  7. app.tgz 含 bin/easytier-pro-fnos、ui/index.html、ui/config、ui/images/icon_64.png
#  8. 泄露检查：app.tgz 不含 ui/src、node_modules、package.json、tsconfig*、
#     vite.config.*、embed.go、*.test.*、*.map
#  9. checksum 与 app.tgz 实际 md5 一致
```

- [ ] **Step 3: test-lifecycle.sh**

```sh
#!/bin/sh
# Simulate the fnOS app lifecycle locally (no fnOS required).
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
DEV=$(mktemp -d)
trap 'rm -rf "$DEV"' EXIT

# 1. build daemon into a fake target tree
mkdir -p "$DEV/target/bin" "$DEV/var"
CGO_ENABLED=0 go build -o "$DEV/target/bin/easytier-pro-fnos" "$ROOT/cmd/easytier-pro-fnos"
cp -r "$ROOT/fpk/cmd" "$DEV/cmd" && chmod 755 "$DEV/cmd/"*

# 2. fake fnOS env
export TRIM_APPNAME=easytier-pro TRIM_APPVER=0.1.0
export TRIM_APPDEST="$DEV/target" TRIM_APPDEST_VOL="$DEV" \
       TRIM_PKGVAR="$DEV/var" TRIM_PKGETC="$DEV/etc" TRIM_PKGTMP="$DEV/tmp" \
       TRIM_TEMP_LOGFILE="$DEV/install.log"
export ETP_DEV_NO_FNOS_AUTH=1

# 3. install → start → status → API → stop → status → uninstall
"$DEV/cmd/install_init"
"$DEV/cmd/install_callback"
"$DEV/cmd/main" start
sleep 1
"$DEV/cmd/main" status
curl -sf --unix-socket "$DEV/target/app.sock" http://localhost/api/status | grep -q '"ok":true'
"$DEV/cmd/main" stop
if "$DEV/cmd/main" status; then echo "still running" >&2; exit 1; fi
TRIM_APP_STATUS=UNINSTALL "$DEV/cmd/uninstall_init"
TRIM_APP_STATUS=UNINSTALL "$DEV/cmd/uninstall_callback"
[ ! -d "$DEV/var/state" ] || { echo "state not removed" >&2; exit 1; }
echo "lifecycle OK"
```

注意：dev 模式下 daemon 需把 `ETP_DEV_NO_FNOS_AUTH` 视为隐含 dev——Task 1 的 `fnosenv.New` 只在 `config.DevMode()`（即 `ETP_DEV_ROOT` 非空）时才 bypass，因此脚本需额外 `export ETP_DEV_ROOT="$DEV"`。daemon 的 Unix socket 路径来自 `controlSocket(paths)` = `$TRIM_APPDEST/app.sock`，与 curl 目标一致。

- [ ] **Step 4: 实跑三个脚本**

```bash
sh scripts/test-lifecycle.sh                 # 预期末尾输出 lifecycle OK
scripts/build-fpk.sh --arch all              # 预期 dist/ 下两个 fpk
scripts/check-fpk.sh dist/*.fpk              # 预期全部通过
```

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat(scripts): add fpk build, check and lifecycle simulation"
```

---

### Task 9: README、CI 与收尾

**Files:**
- Modify: `README.md`（补全：功能列表、手动安装指引、开发/构建/测试命令、目录说明）
- Create: `.github/workflows/checks.yml`（go vet+test、sh -n、UI test+build、build-fpk+check-fpk）
- Create: `.github/workflows/build.yml`（main 分支构建 fpk artifact，参照源仓库）

**Interfaces:**
- Consumes: 全部前序任务的脚本与命令。

- [ ] **Step 1: README（中文）补全**：项目简介、功能清单（对齐 synology README 结构）、手动安装（应用中心→手动安装→上传 fpk）、从源码构建（`scripts/build-fpk.sh`）、本地生命周期测试（`scripts/test-lifecycle.sh`）、真机验证清单（引用设计文档 §12）、目录结构说明。

- [ ] **Step 2: CI workflows**（参照源仓库 `.github/workflows/checks.yml`，去掉过时的 `node --test ui/lib/*.test.mjs` 行）：

checks.yml 步骤：`go vet ./...` → `go test ./...` → `sh -n scripts/*.sh fpk/cmd/*` → `(cd ui && npm ci && npm run test && npm run build)` → `scripts/build-fpk.sh --arch all --skip-ui`（如 build 脚本无 --skip-ui 则直接全量）→ `scripts/check-fpk.sh dist/*.fpk` → 泄露 grep 回执（同 Global Constraints 的关键词集合，全仓排除 .git）。

build.yml：main push 时构建并上传 `dist/*.fpk` artifact（照源仓库 build.yml 结构）。

- [ ] **Step 3: 最终全量验证**

```bash
go vet ./... && go test ./...
sh -n scripts/*.sh fpk/cmd/*
(cd ui && npm run test && npm run build)
scripts/build-fpk.sh --arch all && scripts/check-fpk.sh dist/*.fpk
sh scripts/test-lifecycle.sh
grep -RIn --exclude-dir=.git --exclude-dir=node_modules -e 'easytier-pro-dsm' -e 'SYNOPKG' -e 'X-Syno' -e '群晖' . || echo CLEAN
```

（`dsm`/`DSM`/`synology` 全词在 README/文档中提及移植来源时可出现；代码与打包物中不得出现。）

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "docs: add README and CI workflows"
```

---

## 真机验证清单（交付时附，非本计划任务）

1. fnOS ≥ 1.2.0401 设备上手动安装 x86_64 fpk，确认桌面图标出现、点击可打开 iframe UI。
2. 确认统一网关转发行为（是否剥离 `/app/easytier-pro` 前缀）——双挂路由已兜底，验证后可在后续版本收敛。
3. 确认 nginx worker 可读写 `$TRIM_APPDEST/app.sock`（0666 已兜底，记录实际用户/组供后续收紧）。
4. 确认 `/dev/net/tun` 存在且 core 可创建虚拟网卡。
5. 完整走一遍：device flow 登录 → 激活工作区 → 触发运行时下载 → 加入网络 → 状态/日志展示 → 停止/启动 → 卸载（确认 state 清理）。
