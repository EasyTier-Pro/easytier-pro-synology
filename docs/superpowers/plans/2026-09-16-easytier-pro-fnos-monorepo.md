# EasyTier Pro fnOS 应用实现计划（v2，monorepo）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 easytier-pro-synology 仓库内完成 monorepo 改造，同时产出群晖 .spk 与 fnOS .fpk 两个平台包，功能完全对齐，并在真实 fnOS（PVE VM 143）上完成新用户全流程 E2E 后交付。

**Architecture:** Go 1.24 零依赖共享 daemon（supervise/serve）+ 单份 Vue3 UI 源码双构建产物；平台差异收敛到 `internal/platform`（监听器/鉴权器/品牌名）、`internal/config` 路径根、`internal/{dsmenv,fnosenv}` 与构建期 `VITE_PLATFORM` 注入。

**Tech Stack:** Go 1.24（纯 stdlib）、Vue 3.5 + naive-ui + Vite 7 + TypeScript、bash 生命周期脚本、tar.gz 格式 .fpk、SPK。

**设计文档：** `docs/superpowers/specs/2026-09-16-easytier-pro-fnos-design.md`（权威，冲突时以设计文档为准并回改计划）

**移植源状态：** `/data/project/easytier-pro-fnos` 独立仓已完成 Task 1-4（config/console/runtime 移植验证），证明 verbatim 复用可行；monorepo 改造将吸收这些结论但直接在共享代码上做。

**E2E 环境：** PVE VM 143（fnOS，IP 10.147.223.159，admin 账号 e2e / E2e-test-2026!，SSH 已开，正在从 0.8.47 在线升级到 ≥1.1.15）。

## Global Constraints

- 模块路径 `github.com/EasyTier-Pro/easytier-pro-dsm` 不变（避免全仓 import churn）。
- 群晖版零回归：`scripts/build-spk.sh`、`scripts/check-spk.sh`、`tests/smoke-lifecycle.sh`、`go test ./...`、`ui/dist-dsm` 与现有产物结构完全一致。
- daemon 二进制名：`easytier-pro-dsm`（群晖）、`easytier-pro-fnos`（fnOS）。
- 错误码：`dsm_auth_required`/`dsm_auth_forbidden`（群晖，已有）、`fnos_auth_required`/`fnos_auth_forbidden`（fnOS，新增），中文消息 `"请先登录 fnOS。"` / `"只有 fnOS 管理员可以使用该功能。"`。
- 用户可见文案中文、代码注释英文。
- fnOS 网关前缀 `/app/easytier-pro`，Unix socket `$TRIM_APPDEST/app.sock`，UI vite base `/app/easytier-pro/`。
- core 启动契约：`ET_RPC_PORTAL=0.0.0.0:15888`、`ET_RPC_PORTAL_WHITELIST=127.0.0.0/8,::1/128`、`ET_CONSOLE_LOG_LEVEL=off`、`easytier-core --secure-mode=true`，core stdout/stderr 丢弃。
- fnOS `run-as: root`；平台仅 x86_64（manifest `platform=x86`）与 arm64（`platform=arm`）两个包。
- 默认 Console `https://api.console.easytier.net`；core 版本永远取 Console `stable.version`。
- 生命周期脚本幂等；用户可见错误写 `$TRIM_TEMP_LOGFILE` 并非零退出。
- `ui/dist-dsm` 与 `ui/dist-fnos` 均入库；`dist/`、`.dev/`、`node_modules` 进 .gitignore。
- git author：`KKRainbow <5665404+KKRainbow@users.noreply.github.com>`；commit message 为 Conventional-Commits 风格、英文、textwidth 72。
- **回执与泄露纪律：** 平台拆分后必须 `grep -RIn -e 'easytier-pro-dsm' -e 'dsm' -e 'DSM' -e 'synology' -e 'Synology' -e 'SYNOPKG' -e '群晖' -e 'X-Syno'` 对 `internal/fnosenv/`、`internal/platform/fnos.go`、`cmd/easytier-pro-fnos/`、`ui/src/platform/fnos.ts`、`fpk/` 执行零命中回执；`check-fpk.sh` 与 CI 必须含防泄露检查。
- 每个任务结束提交一次 commit（消息见各任务最后一步）。

---

### Task M2: monorepo 骨架重构（群晖零回归）

**Files:**
- Create: `internal/platform/platform.go`（Platform struct + 注册表）
- Create: `internal/platform/dsm.go`（`//go:build !fnos` 或默认）
- Create: `internal/daemon/daemon.go`（supervise/serve 脚手架）
- Modify: `cmd/easytier-pro-dsm/main.go`（收敛为薄入口）
- Modify: `internal/config/paths.go` → 拆 `paths.go`（共享派生方法）+ `paths_dsm.go`（`ResolvePaths` 读 SYNOPKG_*）
- Modify: `internal/httpserver/server.go`（`requireDSMSession` → `requireSession`，鉴权器接口 + 注入错误码）
- Modify: `internal/apperr/apperr.go`（新增 `CodeFnOSAuthRequired`/`CodeFnOSAuthForbidden`）
- Modify: `internal/console/enroll.go`、`internal/runtime/manager.go`（品牌名 `"Synology NAS"` → `platform.DisplayName`）

**Interfaces:**
- Produces（后续任务依赖）:
  - `platform.Current Platform`（构建期选定）
  - `type Platform struct { Name string; DisplayName string; ListenAddr func(config.Paths) (string, net.Listener, error); NewAuth func(*config.Logger) httpserver.Authenticator; AuthRequiredCode, AuthForbiddenCode string }`
  - `httpserver.Authenticator` 接口：`Authenticate(ctx, *http.Request) (string, *apperr.Error)` + `Bypassed() bool`
  - `daemon.Run(paths config.Paths, log *config.Logger, uiFS fs.FS) error`（supervise+serve 主体）
  - `config.ResolvePaths()`（dsm 版：读 SYNOPKG_*，DevRoot 回退）

- [ ] **Step 1: 分支与准备**

```bash
cd /data/project/easytier-pro-synology
git checkout -b monorepo-fnos
```

- [ ] **Step 2: apperr 新增 fnOS 码**

`internal/apperr/apperr.go` 追加（不动现有码）：

```go
CodeFnOSAuthRequired  = "fnos_auth_required"   // "请先登录 fnOS。"
CodeFnOSAuthForbidden = "fnos_auth_forbidden"  // "只有 fnOS 管理员可以使用该功能。"
```

- [ ] **Step 3: config/paths.go 拆分**

`paths.go` 保留全部派生方法与 `Paths` 结构；新建 `paths_dsm.go`：

```go
//go:build !fnos

package config

// ResolvePaths reads the Synology package env (SYNOPKG_PKGDEST / SYNOPKG_PKGVAR),
// falling back to ETP_DEV_ROOT for development.
func ResolvePaths() (Paths, error) { ... }  // 现状逻辑原样
```

`paths.go` 顶部删除 `ResolvePaths`/`DevRoot`/`DevMode`（移入 paths_dsm.go）。`go build ./...`（默认 tag，即 dsm）必须通过。

- [ ] **Step 4: platform 包**

`internal/platform/platform.go`：

```go
// Package platform abstracts the NAS-specific wiring between the shared daemon
// and the host operating system (Synology DSM or fnOS).
package platform

import (
	"net"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/httpserver"
)

// Platform carries the per-OS integration points the daemon needs.
type Platform struct {
	// Name is the build-time identifier ("dsm" or "fnos").
	Name string
	// DisplayName is used in enrollment key names and logs, e.g. "Synology NAS".
	DisplayName string
	// ListenAddr returns the address/socket path and a ready listener.
	ListenAddr func(paths config.Paths) (string, net.Listener, error)
	// NewAuth builds the session authenticator for the platform gateway.
	NewAuth func(log *config.Logger) httpserver.Authenticator
	// AuthRequiredCode / AuthForbiddenCode are the apperr codes the gateway
	// middleware maps to 401 / 403.
	AuthRequiredCode  string
	AuthForbiddenCode string
}

// Current is the platform selected at build time (dsm by default, fnos with
// the "fnos" build tag).
var Current Platform
```

`internal/platform/dsm.go`：

```go
//go:build !fnos

package platform

import (
	"fmt"
	"net"
	"os"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/apperr"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/dsmenv"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/httpserver"
)

const listenAddress = "127.0.0.1:15890"

func init() {
	Current = Platform{
		Name:              "dsm",
		DisplayName:       "Synology NAS",
		ListenAddr:        listenDSM,
		NewAuth:           func(log *config.Logger) httpserver.Authenticator { return dsmenv.New(log) },
		AuthRequiredCode:  apperr.CodeDSMAuthRequired,
		AuthForbiddenCode: apperr.CodeDSMAuthForbidden,
	}
}

func listenDSM(paths config.Paths) (string, net.Listener, error) {
	addr := listenAddress
	if override := os.Getenv("ETP_DEV_LISTEN"); config.DevMode() && override != "" {
		addr = override
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, fmt.Errorf("listen %s: %w", addr, err)
	}
	return addr, ln, nil
}
```

- [ ] **Step 5: httpserver 鉴权泛化**

`internal/httpserver/server.go`：
1. 定义接口：

```go
// Authenticator validates a request that reached the daemon through the NAS
// gateway and returns the caller identity or an apperr.Error.
type Authenticator interface {
	Authenticate(ctx context.Context, r *http.Request) (string, *apperr.Error)
	Bypassed() bool
}
```

2. `Server` 结构体 `auth` 字段类型改为 `Authenticator`；`New` 签名加 `requiredCode, forbiddenCode string`（或改为接受 `platform.Platform`，权衡：为避免 import 环，httpsserver 不依赖 platform，只依赖接口与两个码——选注入两个码）。
3. `requireDSMSession` → `requireSession`：

```go
func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := checkSameOrigin(r); err != nil { ... }
		identity, authErr := s.auth.Authenticate(r.Context(), r)
		if authErr != nil {
			status := http.StatusInternalServerError
			switch authErr.Code {
			case s.authRequiredCode:  status = http.StatusUnauthorized
			case s.authForbiddenCode: status = http.StatusForbidden
			}
			writeError(w, status, authErr)
			return
		}
		_ = identity
		next.ServeHTTP(w, r)
	})
}
```

4. `Handler()` 里 `root.Handle("/api/", s.requireSession(api))`。

- [ ] **Step 6: daemon 脚手架提取**

新建 `internal/daemon/daemon.go`，把 `cmd/easytier-pro-dsm/main.go` 的 `runSupervise`/`runServe`/`stopChild`/`writePIDFile`/respawn 常量全部搬入，导出：

```go
// Run executes the supervise or serve command for the current platform.
func Run(command string, paths config.Paths, buildVersion string) error
```

`cmd/easytier-pro-dsm/main.go` 收敛为：

```go
var buildVersion = "dev"

func main() {
	command := "supervise"
	if len(os.Args) > 1 { command = os.Args[1] }
	paths, err := config.ResolvePaths()
	if err != nil { ... }
	if command == "version" { fmt.Printf("easytier-pro-dsm %s\n", buildVersion); return }
	if err := daemon.Run(command, paths, buildVersion); err != nil { ... }
}
```

- [ ] **Step 7: 品牌名参数化**

`internal/console/enroll.go`、`internal/runtime/manager.go` 中 `"Synology NAS"` → `platform.DisplayName`（import platform 包；若产生 import 环则改为在 NewManager/New 时注入 brand string——优先尝试 platform 包直引，console/runtime 不依赖 platform 的 Listener/Auth，只用常量）。

- [ ] **Step 8: 群晖全量回归验证**

```bash
cd /data/project/easytier-pro-synology
go vet ./... && go test ./...
scripts/build-spk.sh --dsm all --arch all
scripts/check-spk.sh dist/*.spk
sh tests/smoke-lifecycle.sh
```

预期：全部通过；`git diff main --stat` 只触及计划内文件；`ui/dist` 未被触碰（本任务不改 UI）。

- [ ] **Step 9: Commit**

```bash
git add -A && git commit -m "refactor: extract platform adapter and daemon scaffolding for monorepo"
```

---

### Task M3: UI 平台化拆分

**Files:**
- Modify: `ui/vite.config.ts`（base 由 `VITE_PLATFORM` 决定，输出目录 `dist-<platform>`）
- Modify: `ui/package.json`（scripts: `build:dsm` / `build:fnos`，`name` 改 `easytier-pro-nas-ui`）
- Create: `ui/src/platform/index.ts`（按 `import.meta.env.VITE_PLATFORM` 导出当前平台配置）
- Create: `ui/src/platform/dsm.ts`、`ui/src/platform/fnos.ts`
- Modify: `ui/src/api/client.ts`（`synoToken()` 移入 platform/dsm.ts；401/403 映射用平台码；保留 `X-Easytier-Request`）
- Modify: `ui/src/api/errors.ts`（码表 + needsRelogin 用平台码）
- Modify: `ui/src/views/OverviewView.vue`、`NetworksView.vue`、`SettingsView.vue`、`LogsView.vue`、`components/TunNotice.vue`、`components/DeviceLoginDialog.vue`（重登链接、文案走平台配置）
- Create: `ui/embed_dsm.go`（`//go:build !fnos`，embed dist-dsm）、`ui/embed_fnos.go`（`//go:build fnos`，embed dist-fnos）；删除 `ui/embed.go`
- Git: `git mv ui/dist ui/dist-dsm`（保留历史）

**Interfaces:**
- Consumes: M2 的 `httpserver`（路由不变）、`platform.Current`。
- Produces:
  - `ui.FS fs.FS`（两个 embed 文件均导出同名变量，按 tag 二选一）
  - `npm run build:dsm` / `npm run build:fnos`
  - `ui/src/platform/index.ts` 导出：

```ts
export interface PlatformConfig {
  /** vite base, e.g. './' or '/app/easytier-pro/' */
  base: string
  /** extra request headers injected by the platform shell */
  extraHeaders(): Record<string, string>
  /** error codes for 401/403 */
  authRequiredCode: string
  authForbiddenCode: string
  /** where the relogin button points (target=_top) */
  reloginURL: string
  /** brand name for user-visible text */
  brandName: string
  /** whether to show the DSM-style TUN privilege notice */
  showTunNotice: boolean
}
export const platform: PlatformConfig
```

- [ ] **Step 1: git mv ui/dist ui/dist-dsm**

```bash
cd /data/project/easytier-pro-synology
git mv ui/dist ui/dist-dsm
```

- [ ] **Step 2: platform 配置模块**

`ui/src/platform/dsm.ts`：

```ts
import type { PlatformConfig } from './index'

export const platform: PlatformConfig = {
  base: './',
  extraHeaders() {
    const token = synoToken()
    return token ? { 'X-Syno-Token': token } : {}
  },
  authRequiredCode: 'dsm_auth_required',
  authForbiddenCode: 'dsm_auth_forbidden',
  reloginURL: '/webman/index.cgi',
  brandName: 'DSM',
  showTunNotice: true,
}

function synoToken(): string { /* 原 client.ts 32-58 行逻辑原样搬入 */ }
```

`ui/src/platform/fnos.ts`：

```ts
import type { PlatformConfig } from './index'

export const platform: PlatformConfig = {
  base: '/app/easytier-pro/',
  extraHeaders() { return {} },
  authRequiredCode: 'fnos_auth_required',
  authForbiddenCode: 'fnos_auth_forbidden',
  reloginURL: '/',
  brandName: 'fnOS',
  showTunNotice: false,
}
```

`ui/src/platform/index.ts`：

```ts
export interface PlatformConfig { ... }  // 见上

import { platform as dsm } from './dsm'
import { platform as fnos } from './fnos'

export const platform: PlatformConfig =
  import.meta.env.VITE_PLATFORM === 'fnos' ? fnos : dsm
```

- [ ] **Step 3: vite.config.ts**

```ts
const platform = process.env.VITE_PLATFORM === 'fnos' ? 'fnos' : 'dsm'
export default defineConfig({
  base: platform === 'fnos' ? '/app/easytier-pro/' : './',
  build: { outDir: `dist-${platform}`, ... },
  ...
})
```

- [ ] **Step 4: package.json**

```json
{
  "name": "easytier-pro-nas-ui",
  "scripts": {
    "build:dsm": "VITE_PLATFORM=dsm vue-tsc --noEmit && vite build",
    "build:fnos": "VITE_PLATFORM=fnos vue-tsc --noEmit && vite build",
    "build": "npm run build:dsm",
    "test": "vitest run",
    ...
  }
}
```

- [ ] **Step 5: client.ts / errors.ts 改造**

`client.ts`：删 `synoToken()`（已移 platform/dsm.ts）；请求头改为 `...platform.extraHeaders()`；401/403 映射 `platform.authRequiredCode/authForbiddenCode`。
`errors.ts`：码表加 `fnos_auth_required`/`fnos_auth_forbidden` 中文消息；`needsRelogin` 用 `platform.authRequiredCode`。

- [ ] **Step 6: 视图/组件改造**

逐处 Edit（先 grep 定位）：所有 `href="/webman/index.cgi"` → `platform.reloginURL`（`target="_top"`）；DSM 文案 → `platform.brandName` 拼写；`TunNotice.vue` 仅在 `platform.showTunNotice` 时渲染；`DeviceLoginDialog.vue` "请重新登录 DSM" → `请重新登录 ${platform.brandName}`。测试 fixture 里的 `dsm_auth_*` 期望保留（dsm 测试用 dsm 平台跑，fnos 侧后续任务补测试）。

- [ ] **Step 7: embed 与构建**

`ui/embed_dsm.go`：

```go
//go:build !fnos

package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist-dsm
var dist embed.FS

// FS holds the interface assets rooted at the top level.
var FS fs.FS = mustSub()

func mustSub() fs.FS {
	sub, err := fs.Sub(dist, "dist-dsm")
	if err != nil { panic(err) }
	return sub
}
```

`ui/embed_fnos.go`：

```go
//go:build fnos

package ui

//go:embed all:dist-fnos
var dist embed.FS

var FS fs.FS = mustSubFnos()

func mustSubFnos() fs.FS {
	sub, err := fs.Sub(dist, "dist-fnos")
	if err != nil { panic(err) }
	return sub
}
```

- [ ] **Step 8: 双构建 + 测试 + 回执**

```bash
cd ui
npm ci --no-audit --no-fund
npm run test
npm run build:dsm && test -f dist-dsm/index.html
npm run build:fnos && test -f dist-fnos/index.html
grep -RIn -e 'dsm_auth' -e 'X-Syno' -e 'webman' src/platform/fnos.ts dist-fnos/ || echo CLEAN
cd ..
go build ./... && go test ./...
scripts/build-spk.sh --dsm all --arch all   # embed 路径变了，spk 构建须适配 dist-dsm
scripts/check-spk.sh dist/*.spk
sh tests/smoke-lifecycle.sh
```

注意：`scripts/build-spk.sh` 里 `cp -r ui/dist/.` 要改成 `ui/dist-dsm/.`（M2 未动 build-spk，这里适配）。

- [ ] **Step 9: Commit**

```bash
git add -A && git commit -m "feat(ui): split platform config for dsm and fnos builds"
```

---

### Task M4: fnOS 平台后端（fnosenv + platform/fnos + cmd/fnos + httpserver 前缀）

**Files:**
- Create: `internal/config/paths_fnos.go`（`//go:build fnos`，读 TRIM_*）
- Create: `internal/fnosenv/fnosenv.go` + `fnosenv_test.go`
- Create: `internal/platform/fnos.go`（`//go:build fnos`）
- Create: `cmd/easytier-pro-fnos/main.go`
- Modify: `internal/httpserver/server.go`（网关前缀双挂）

**Interfaces:**
- Consumes: M2 的 `platform.Platform`、`httpserver.Authenticator`、daemon 脚手架；M3 的 `ui.FS`（fnos tag）。
- Produces:
  - `fnosenv.Authenticator`（New/Bypassed/Authenticate，形状与 dsmenv 一致）
  - `platform.Current`（fnos tag 下为 fnOS 实现：Unix socket + fnosenv + fnos_auth_* 码）
  - `cmd/easytier-pro-fnos` 二进制（`go build -tags fnos ./cmd/easytier-pro-fnos`）
  - httpserver 路由同时挂 `/api/*` 与 `/app/easytier-pro/api/*`

- [ ] **Step 1: config/paths_fnos.go**

```go
//go:build fnos

package config

// ResolvePaths reads the fnOS package env (TRIM_APPDEST / TRIM_PKGVAR),
// falling back to ETP_DEV_ROOT for development.
func ResolvePaths() (Paths, error) {
	pkgDest := os.Getenv("TRIM_APPDEST")
	pkgVar := os.Getenv("TRIM_PKGVAR")
	if devRoot := DevRoot(); devRoot != "" {
		pkgDest = filepath.Join(devRoot, "target")
		pkgVar = filepath.Join(devRoot, "var")
	}
	if pkgDest == "" || pkgVar == "" {
		return Paths{}, errors.New("TRIM_APPDEST and TRIM_PKGVAR must be set")
	}
	return Paths{PkgDest: pkgDest, PkgVar: pkgVar}, nil
}
```

- [ ] **Step 2: fnosenv（TDD）**

`internal/fnosenv/fnosenv_test.go`：

```go
func TestRejectsMissingHeader(t *testing.T)      // 无 X-Trim-Isadmin → CodeFnOSAuthRequired
func TestRejectsNonAdmin(t *testing.T)           // X-Trim-Isadmin: false → CodeFnOSAuthForbidden
func TestAcceptsAdmin(t *testing.T)              // true → identity = X-Trim-Username
func TestUsernameFallback(t *testing.T)          // isadmin=true 且无 username → "admin"
func TestUsernameSanitized(t *testing.T)         // 控制字符/超长 → 截断或净化
func TestDevBypass(t *testing.T)                 // ETP_DEV_ROOT + ETP_DEV_NO_FNOS_AUTH=1 → Bypassed()==true
func TestNoBypassOutsideDev(t *testing.T)        // 仅 ETP_DEV_NO_FNOS_AUTH=1 → 不 bypass
```

`internal/fnosenv/fnosenv.go`：

```go
// Package fnosenv integrates with the fnOS gateway identity headers.
package fnosenv

// Authenticator trusts the identity headers injected by the fnOS gateway.
type Authenticator struct { bypass bool; log *config.Logger }

// New enables the dev bypass only in dev mode with an explicit opt-in.
func New(log *config.Logger) *Authenticator {
	return &Authenticator{
		bypass: config.DevMode() && os.Getenv("ETP_DEV_NO_FNOS_AUTH") == "1",
		log:    log,
	}
}

func (a *Authenticator) Bypassed() bool { return a.bypass }

func (a *Authenticator) Authenticate(ctx context.Context, r *http.Request) (string, *apperr.Error) {
	if a.bypass { return "dev", nil }
	switch r.Header.Get("X-Trim-Isadmin") {
	case "true":
		name := sanitizeUsername(r.Header.Get("X-Trim-Username"))
		if name == "" { name = "admin" }
		return name, nil
	case "":
		return "", apperr.New(apperr.CodeFnOSAuthRequired)
	default:
		return "", apperr.New(apperr.CodeFnOSAuthForbidden)
	}
}

func sanitizeUsername(s string) string { /* 去控制字符，限 64 字节 */ }
```

- [ ] **Step 3: platform/fnos.go**

```go
//go:build fnos

package platform

func init() {
	Current = Platform{
		Name:              "fnos",
		DisplayName:       "fnOS NAS",
		ListenAddr:        listenFnOS,
		NewAuth:           func(log *config.Logger) httpserver.Authenticator { return fnosenv.New(log) },
		AuthRequiredCode:  apperr.CodeFnOSAuthRequired,
		AuthForbiddenCode: apperr.CodeFnOSAuthForbidden,
	}
}

// listenFnOS creates the Unix socket the fnOS gateway proxies to.
func listenFnOS(paths config.Paths) (string, net.Listener, error) {
	sock := filepath.Join(paths.PkgDest, "app.sock")
	if override := os.Getenv("ETP_DEV_LISTEN"); config.DevMode() && override != "" {
		sock = override
	}
	if err := os.MkdirAll(filepath.Dir(sock), 0o755); err != nil { return "", nil, err }
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil { return "", nil, fmt.Errorf("listen %s: %w", sock, err) }
	// The fnOS gateway (nginx worker) must be able to connect.
	if err := os.Chmod(sock, 0o666); err != nil { ln.Close(); return "", nil, err }
	return sock, ln, nil
}
```

- [ ] **Step 4: httpserver 前缀双挂**

`Handler()` 末尾：

```go
	root := http.NewServeMux()
	root.Handle("/api/", s.requireSession(api))
	root.Handle("/", s.staticHandler())

	// The fnOS gateway may or may not strip the gateway prefix before
	// proxying; accept both forms until verified on real hardware.
	if platform.Current.Name == "fnos" {
		mux := http.NewServeMux()
		mux.Handle("/", root)
		mux.Handle("/app/easytier-pro/", http.StripPrefix("/app/easytier-pro", root))
		return mux
	}
	return root
```

（httpserver import platform——若 M2 未引入该依赖则此处引入；platform 不依赖 httpserver 的 Server，只定义 Authenticator 接口在 httpserver，platform 依赖接口，无环。）

- [ ] **Step 5: cmd/easytier-pro-fnos/main.go**

```go
var buildVersion = "dev"

func main() {
	command := "supervise"
	if len(os.Args) > 1 { command = os.Args[1] }
	paths, err := config.ResolvePaths()
	if err != nil { ... }
	if command == "version" { fmt.Printf("easytier-pro-fnos %s\n", buildVersion); return }
	if err := daemon.Run(command, paths, buildVersion); err != nil { ... }
}
```

- [ ] **Step 6: 双平台编译 + 测试 + 冒烟**

```bash
cd /data/project/easytier-pro-synology
go vet ./... && go test ./...
go build ./...                                    # dsm 默认
go build -tags fnos ./...                         # fnos
CGO_ENABLED=0 go build -tags fnos -o /tmp/easytier-pro-fnos ./cmd/easytier-pro-fnos
ETP_DEV_ROOT=$(mktemp -d) ETP_DEV_NO_FNOS_AUTH=1 /tmp/easytier-pro-fnos supervise &
sleep 1; curl --unix-socket $ETP_DEV_ROOT/target/app.sock http://localhost/api/status | jq .
kill %1
```

预期：双平台编译通过；测试全绿；fnos daemon 经 unix socket 响应 `{"ok":true,...}`。

- [ ] **Step 7: Commit**

```bash
git add -A && git commit -m "feat(fnos): add fnOS platform adapter, auth and daemon entry"
```

---

### Task M5: fpk 打包层 + 构建脚本

**Files:**
- Create: `fpk/manifest`
- Create: `fpk/config/{privilege,resource}`
- Create: `fpk/cmd/{install_init,install_callback,main,upgrade_init,upgrade_callback,uninstall_init,uninstall_callback}`（755）
- Create: `fpk/app/ui/config`、`fpk/app/ui/images/`（图标集）
- Create: `fpk/ICON.PNG`、`fpk/ICON_256.PNG`
- Create: `fpk/wizard/.gitkeep`
- Create: `scripts/build-fpk.sh`、`scripts/check-fpk.sh`、`scripts/test-lifecycle.sh`
- Modify: `scripts/build-spk.sh`（`ui/dist` → `ui/dist-dsm`，M3 已适配则跳过）

**Interfaces:**
- Consumes: M4 的 fnos daemon 二进制、M3 的 `ui/dist-fnos`、网关前缀与 socket 名。
- Produces:
  - `dist/easytier-pro-<ver>-<x86_64|arm64>.fpk`
  - `scripts/build-fpk.sh [--arch x86_64|arm64|all] [--version x.y.z] [--out dist]`
  - `scripts/check-fpk.sh <fpk...>`
  - `scripts/test-lifecycle.sh`（本机模拟 TRIM_* 全生命周期）

- [ ] **Step 1: fpk 源树**

`fpk/manifest`：

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

`fpk/config/privilege`：`{"defaults": {"run-as": "root"}}`
`fpk/config/resource`：`{}`

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

图标：`cp spk/icons/PACKAGE_ICON.PNG fpk/ICON.PNG`、`cp spk/icons/PACKAGE_ICON_256.PNG fpk/ICON_256.PNG`、`cp ui/public/images/app_*.png fpk/app/ui/images/` 并重命名 `app_N.png` → `icon_N.png`。

- [ ] **Step 2: cmd/ 脚本**

`fpk/cmd/main`（start/stop/status）：

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
	mkdir -p "$TRIM_PKGVAR/run" "$TRIM_PKGVAR/logs"
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
		if daemon_pid >/dev/null; then exit 0; fi
		exit 3
		;;
	*) echo "usage: $0 {start|stop|status}" >&2; exit 1 ;;
esac
```

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

`fpk/cmd/upgrade_init`：内联停服逻辑（同 stop_daemon 但不 exit 0 于已停）。`upgrade_callback`：`exit 0`。
`fpk/cmd/uninstall_init`：同 upgrade_init 停服。
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

- [ ] **Step 3: build-fpk.sh**

```sh
#!/bin/sh
# Build easytier-pro .fpk packages for fnOS.
set -eu
# usage: build-fpk.sh [--arch x86_64|arm64|all] [--version X.Y.Z] [--out DIR]

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

VERSION=${VERSION:-$(sed -n 's/^version=\(.*\)$/\1/p' fpk/manifest)}
OUT=${OUT:-dist}
ARCHES="x86_64 arm64"
# arg parse: --arch narrows ARCHES, --version overrides VERSION, --out overrides OUT

build_ui() {
	( cd ui && npm ci --no-audit --no-fund && npm run build:fnos )
	[ -f ui/dist-fnos/index.html ] || { echo "ui build missing dist-fnos/index.html" >&2; exit 1; }
}

build_one() {  # $1 = x86_64|arm64
	case "$1" in
		x86_64) GOARCH=amd64; PLATFORM=x86 ;;
		arm64)  GOARCH=arm64; PLATFORM=arm ;;
	esac
	work=$(mktemp -d)
	trap 'rm -rf "$work"' EXIT
	mkdir -p "$work/app/bin"
	CGO_ENABLED=0 GOOS=linux GOARCH=$GOARCH go build -tags fnos -trimpath \
		-ldflags "-s -w -X main.buildVersion=$VERSION" \
		-o "$work/app/bin/easytier-pro-fnos" ./cmd/easytier-pro-fnos
	cp -r ui/dist-fnos/. "$work/app/ui/"
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
	mkdir -p "$OUT"
	( cd "$work" && tar czf "$ROOT/$OUT/easytier-pro-$1-$VERSION.fpk" \
		manifest app.tgz cmd config wizard ICON.PNG ICON_256.PNG LICENSE )
}
```

若 `command -v fnpack` 存在则优先 `fnpack build`（社区验证格式等价），否则手工 tar。

- [ ] **Step 4: check-fpk.sh**

对照 `scripts/check-spk.sh` 结构，校验：
1. tar 成员齐全（manifest app.tgz cmd/* config/* wizard ICON* LICENSE）。
2. manifest 必填键（appname version display_name source platform os_min_version ctl_stop desktop_uidir desktop_applaunchname checksum 非空且 32 位 md5）。
3. platform ∈ {x86, arm} 且与文件名 `-x86_64`/`-arm64` 一致。
4. `config/privilege` 合法 JSON 且 `defaults.run-as == "root"`。
5. `app.tgz` 内 `ui/config` 合法 JSON 且 `gatewayPrefix == "/app/easytier-pro"`、`gatewaySocket == "app.sock"`。
6. 所有 `cmd/*` 可执行。
7. `app.tgz` 含 `bin/easytier-pro-fnos`、`ui/index.html`、`ui/config`、`ui/images/icon_64.png`。
8. 泄露检查：`app.tgz` 不含 `ui/src`、`node_modules`、`package.json`、`tsconfig*`、`vite.config.*`、`embed*.go`、`*.test.*`、`*.map`、`dsm`、`Synology`、`群晖`、`X-Syno`。
9. `checksum` 与 `app.tgz` 实际 md5 一致。

- [ ] **Step 5: test-lifecycle.sh**

```sh
#!/bin/sh
# Simulate the fnOS app lifecycle locally (no fnOS required).
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
DEV=$(mktemp -d)
trap 'rm -rf "$DEV"' EXIT

mkdir -p "$DEV/target/bin" "$DEV/var"
CGO_ENABLED=0 go build -tags fnos -o "$DEV/target/bin/easytier-pro-fnos" "$ROOT/cmd/easytier-pro-fnos"
cp -r "$ROOT/fpk/cmd" "$DEV/cmd" && chmod 755 "$DEV/cmd/"*

export TRIM_APPNAME=easytier-pro TRIM_APPVER=0.1.0
export TRIM_APPDEST="$DEV/target" TRIM_APPDEST_VOL="$DEV" \
       TRIM_PKGVAR="$DEV/var" TRIM_PKGETC="$DEV/etc" TRIM_PKGTMP="$DEV/tmp" \
       TRIM_TEMP_LOGFILE="$DEV/install.log"
export ETP_DEV_ROOT="$DEV" ETP_DEV_NO_FNOS_AUTH=1

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

- [ ] **Step 6: 实跑三脚本 + 回执**

```bash
sh scripts/test-lifecycle.sh
scripts/build-fpk.sh --arch all
scripts/check-fpk.sh dist/*.fpk
grep -RIn -e 'dsm' -e 'DSM' -e 'Synology' -e 'synology' -e 'SYNOPKG' -e '群晖' -e 'X-Syno' fpk/ scripts/build-fpk.sh scripts/check-fpk.sh || echo CLEAN
```

- [ ] **Step 7: Commit**

```bash
git add -A && git commit -m "feat(fpk): add fnOS package manifest, lifecycle scripts and build tooling"
```

---

### Task M6: CI 双平台

**Files:**
- Modify: `.github/workflows/checks.yml`（加 fnos 编译、fpk 构建+校验、生命周期模拟）
- Modify: `.github/workflows/build.yml`（main 分支构建 spk+fpk artifact）
- Modify: `README.md`（monorepo 说明、双平台构建命令）

**Interfaces:**
- Consumes: 全部前序任务的脚本与命令。

- [ ] **Step 1: checks.yml**

在现有步骤（go vet、go test、sh -n、build-spk、check-spk）后追加：

```yaml
      - name: Build fnOS daemon
        run: CGO_ENABLED=0 go build -tags fnos ./cmd/easytier-pro-fnos
      - name: Build fnOS UI
        run: cd ui && npm run build:fnos
      - name: Build and check fpk
        run: |
          scripts/build-fpk.sh --arch all
          scripts/check-fpk.sh dist/*.fpk
      - name: Lifecycle simulation
        run: sh scripts/test-lifecycle.sh
      - name: Leak check
        run: |
          ! grep -RIn --exclude-dir=.git --exclude-dir=node_modules \
            -e 'easytier-pro-dsm' -e 'SYNOPKG' -e 'X-Syno' -e '群晖' \
            internal/fnosenv/ internal/platform/fnos.go cmd/easytier-pro-fnos/ \
            ui/src/platform/fnos.ts ui/dist-fnos/ fpk/
```

- [ ] **Step 2: build.yml**

main push 时构建 `dist/*.spk dist/*.fpk` 并上传 artifact（照现有结构扩展）。

- [ ] **Step 3: README 更新**

补：monorepo 说明（synology + fnos 共用代码）、双平台构建命令、fnOS 手动安装指引（应用中心→手动安装→上传 fpk）、E2E 环境说明。

- [ ] **Step 4: 验证 + Commit**

```bash
git add -A && git commit -m "ci: build and check both spk and fpk packages"
```

---

### Task M7: fnOS 真机 E2E（交付门槛）

**Files:**
- Create: `docs/e2e-20260916-fnos/README.md`（证据归档）
- Create: `docs/e2e-20260916-fnos/*.png` / `*.txt`（截图与命令输出）

**环境：** PVE VM 143（fnOS，IP 10.147.223.159，admin 账号 e2e / E2e-test-2026!，SSH 已开，系统版本升级到 ≥1.1.15 后需确认 ≥1.2.0401，若在线升级不足则在 VM 内手动下载新版 ISO 重装或继续在线升级）。

**前置：** M5 产出的 `dist/easytier-pro-0.1.0-x86_64.fpk`。

- [ ] **Step 1: 确认系统版本**

```bash
sshpass -p 'E2e-test-2026!' ssh e2e@10.147.223.159 \
  'echo E2e-test-2026! | sudo -S cat /usr/trim/BUILD_VERSION'
```

确认 ≥ 1.2.0401；不足则继续 `liveupdate.update` 或重装。

- [ ] **Step 2: 安装 fpk**

```bash
scp dist/easytier-pro-0.1.0-x86_64.fpk e2e@10.147.223.159:/tmp/
sshpass -p 'E2e-test-2026!' ssh e2e@10.147.223.159 \
  'echo E2e-test-2026! | sudo -S appcenter-cli install-fpk /tmp/easytier-pro-0.1.0-x86_64.fpk'
```

记录输出；失败则排错（版本、结构、依赖）。

- [ ] **Step 3: 桌面图标与 UI 加载**

fnOS 桌面（http://10.147.223.159:5666）以 e2e 登录，确认 EasyTier Pro 图标出现；点击打开 iframe，确认 UI 经统一网关加载（截图）。用 curl 模拟网关请求验证 API：

```bash
# 拿 fnOS session cookie 后
curl -b cookies.txt https://10.147.223.159:5666/app/easytier-pro/api/status
```

（截图经 QMP screendump 采集，或浏览器开发者工具截图——VM 内无浏览器，用 qm screendump + 手动点击。）

- [ ] **Step 4: 新用户完整流程（Console 侧需人工一次确认）**

1. 在 UI 或 API 触发 `POST /api/auth/device/start`，记录 `verification_uri_complete` 与 `user_code`。
2. **人工步骤：** 打开该链接，登录 EasyTier Console 账号，确认设备码。
3. API 轮询 `POST /api/auth/device/poll` 直至 `authenticated`。
4. `POST /api/workspaces/{id}/activate`（默认模式）。
5. `POST /api/runtime/download`（触发 easytier-core 下载），轮询 status 至 `done`。
6. `POST /api/networks/{id}/join`。
7. `GET /api/status` 确认节点在线；`GET /api/logs` 确认日志可读；VM 内 `ip link` 确认 TUN 设备存在。

- [ ] **Step 5: 生命周期**

`appcenter-cli stop easytier-pro` → `status` 退出码 3；`start` → 0；`GET /api/status` 恢复在线。

- [ ] **Step 6: 升级**

重新构建同版本或 bump 0.1.1 → 安装 → 确认 state/secrets 保留、服务自动恢复。

- [ ] **Step 7: 卸载**

`appcenter-cli uninstall easytier-pro` → 确认 `/var/apps/easytier-pro/var/` 下 state/runtime/logs/run/cache 已清理。

- [ ] **Step 8: 证据归档 + Commit**

```bash
git add docs/e2e-20260916-fnos/ && git commit -m "docs(e2e): record fnOS install, onboarding and lifecycle results"
```

---

### Task M8: 最终审查与收尾

**Files:**
- Modify: `/data/project/easytier-pro-fnos/README.md`（标注已迁移至 monorepo，指向 synology 仓）
- 最终全分支审查（`git merge-base main HEAD` → HEAD 的完整 diff）

- [ ] **Step 1: 最终全分支审查**

dispatch final code reviewer（见 subagent-driven-development 的 code-reviewer 模板），范围 `main..monorepo-fnos` 全 diff。

- [ ] **Step 2: 收尾提交**

```bash
cd /data/project/easytier-pro-fnos
git add -A && git commit -m "docs: mark repo as superseded by the monorepo in easytier-pro-synology"
```

- [ ] **Step 3: 交付**

向用户报告：monorepo 分支、spk/fpk 构建产物路径、E2E 证据、遗留事项（fnOS 商店上架、仓库改名建议）。
