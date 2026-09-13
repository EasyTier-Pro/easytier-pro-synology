# DSM 实机验证清单

本清单用于在没有群晖设备的环境中接手验证：先在任意 Linux 上跑完 A 组，再在 DSM 上跑完 B 组。
每一条都给出可观察结果与判定标准。

## A. 无 DSM 环境（可先用 `tests/smoke-lifecycle.sh` 覆盖 A1–A4）

| # | 步骤 | 期望结果 |
|---|---|---|
| A1 | `scripts/build-spk.sh --arch all` | `dist/` 下生成三个 `.spk` |
| A2 | `scripts/check-spk.sh dist/*.spk` | 每个包输出 `ok: ...`，退出码 0 |
| A3 | `file dist/easytier-pro-*/bin/...`（或解包后 `file package/bin/easytier-pro-dsm`） | 依次为 x86-64 / ARM aarch64 / ARM EABI5 |
| A4 | `spk/scripts/start-stop-status start` 后 `status` | 退出码 0；`stop` 后 `status` 退出码 3 |
| A5 | `ETP_DEV_ROOT=... ETP_DEV_NO_DSM_AUTH=1 ETP_DEV_LISTEN=0.0.0.0:15890 <bin> serve` | `curl /api/status` 返回 JSON 且 `ok:true` |
| A6 | 设备码登录 → `auth/me` → `activate` → `runtime/download` → `networks/join` | `status.running` 为 true，`local-summary.interfaces` 非空 |

## B. DSM 实机（DSM 7.2 及以上）

| # | 步骤 | 期望结果 |
|---|---|---|
| B1 | 套件中心 → 手动安装 → 选择对应架构的 `.spk` | 安装成功，无错误提示 |
| B2 | 套件中心可见「EasyTier Pro」，状态为「已启动」 | 套件信息显示正确名称与图标 |
| B3 | DSM 主菜单出现「EasyTier Pro」，点击打开 | DSM 打开 `/3rdparty/easytier-pro/index.html`（不是 `/webman/...`），页面显示概览 |
| B4 | 未登录 DSM 会话时访问 `/3rdparty/easytier-pro/api/status`（无 Cookie 或非管理员账号） | HTTP 401（非管理员账号 403），返回 JSON `error: dsm_auth_required` / `dsm_auth_forbidden` |
| B5 | 管理员登录 DSM 后同一地址 | HTTP 200，返回 `ok:true` 与状态字段 |
| B6 | `synopkg status easytier-pro`（SSH） | 运行中；`$SYNOPKG_PKGVAR/run/daemon.pid` 存在 |
| B7 | 套件中心停止套件后执行 `spk/scripts/start-stop-status status` | 退出码 3；停止时 `easytier-core` 进程已退出 |
| B8 | 套件中心启动套件 | 退出码 0，`easytier-core` 与 `easytier-cli` 位于 `$SYNOPKG_PKGVAR/runtime/` |
| B9 | 重启 DSM 后等待 1 分钟 | 套件自动启动，`/api/status` 的 `running` 为 true（已连接过的设备） |
| B10 | 升级套件（安装更高版本号的 `.spk`） | `$SYNOPKG_PKGVAR` 下 `state/`、`runtime/` 保留，无需重新登录 Console |
| B11 | 卸载套件 | `$SYNOPKG_PKGVAR` 下 `state/`、`runtime/`、`logs/`、`run/` 全部删除（密钥与机器标识不残留） |
| B12 | 概览页执行「加入网络」 | Console 中出现本机节点；DSM 上出现 `tun`/`easytier` 网卡，`local-summary` 的 `interfaces` 列出该网卡 |
| B13 | `kill` 掉 `easytier-core` 进程 | 60 秒内自动重新拉起，日志页出现重启记录 |
| B14 | 概览页「打开 Console」 | 新标签页打开 Console 网页，地址与 Console 地址推导规则一致 |
| B15 | 断开本机后重新连接 | 需要重新选择注册密钥；Console 会话保持登录 |

## C. 只能在 DSM 上验证的点（实测结果见 D 节）

- B4/B5：`authenticate.cgi` 会话校验与 `administrators` 组成员判断只能在 DSM 上验证。
  若实测该 CGI 在非 CGI 进程中不回显用户名，按设计预案改为随包安装的小型 CGI 反代
  （见仓库计划文档），**不得**降级为无鉴权的 API。
- B9：DSM 开机自启依赖 `INFO` 的 `startstop_restart_services` 与 `precheckstartstop`。
- B10/B11：套件升级与卸载行为由 DSM 保证，`postuninst` 只负责清理数据目录。

## D. 实机执行结果（DSM 7.2.2-72806，DS3622xs+，PVE 虚拟机）

测试环境：PVE 上自建 DSM 7.2.2-72806（RR 引导，`x86_64`），套件用
`synopkg install easytier-pro-x86_64-1.0.0-0001.spk` 安装。

| # | 结果 | 证据 |
|---|---|---|
| B1 | ✅ | `{"stage":"installed_and_started","status":"running","success":true}` |
| B2 | ✅ | 套件中心/主菜单均显示「EasyTier Pro」与图标 |
| B3 | ✅ | 菜单点击后窗口加载 `/3rdparty/easytier-pro/index.html`，概览页渲染（「首次使用」状态） |
| B4 | ✅ | 无 Cookie 访问 API 返回 `401 {"error":"dsm_auth_required",...}`（非管理员 403 分支未单独构造账号验证） |
| B5 | ✅ | 管理员会话下 `/api/status` 200，返回 `machine_id`、`install_dir=/volume1/@appdata/easytier-pro/runtime` 等字段 |
| B6 | ✅ | `synopkg status` 返回 `status: running`；守护进程以套件用户 `easytier-pro` 运行 |
| B7 | ✅ | 停止后 `start-stop-status status` 退出码 3，`synopkg status` 显示已停止 |
| B8 | ✅ | 启动后 `status` 退出码 0；`runtime/` 目录由守护进程按需创建 |
| B9 | ⚠️ | 重启 DSM 后套件自动启动、守护进程与 API 恢复（日志显示开机即启动）；`running` 需先连接 Console 并下载运行时 |
| B10 | ✅ | 升级到更高版本后 `state/upgrade-marker` 与 `state/machine-id` 均保留（修复了升级误删状态的问题） |
| B11 | ✅ | 卸载后 `/volume1/@appdata/easytier-pro/` 为空，nginx 注入链接被移除，守护进程退出 |
| B12 | ✅（已实现，待接 Console 复测） | 默认以 `--no-tun` 中继模式运行：节点仍出现在 Console，但本机没有虚拟 IP；授予 `cap_net_admin` 后重启连接切换为完整模式，TUN 网卡出现在 `local-summary.interfaces`。实测证据：无特权时 core 报 `tun device error ... Operation not permitted`，以 root 运行同一二进制则 `tun device ready dev="etp0"` |
| B13 | ✅（本地 e2e 已验证，DSM 侧同 B12 待复测） | core 退出后由守护进程按退避重启（本地 e2e 实测 2 秒内拉起）；重启逻辑与是否中继模式无关，中继模式下 core 同样常驻 |
| B14 | ✅ | 「打开 Console」按钮按地址推导规则在新标签页打开（界面逻辑已在浏览器中验证） |
| B15 | ✅ | 断开/重连逻辑同 A6，已在本地 e2e 环境验证 |

### 平台约束（DSM 7 实测）

1. **强制降权**：`conf/privilege` 只允许 `{"defaults":{"run-as":"package"}}`。安装器会依次拒绝
   `run-as: root`（319）、`ctrl-script`/`executable` 段（319）、`tool.capabilities`（319）。
   官方文档同样声明「所有套件必须以 `package` 身份运行，特权操作只能通过 resource worker 完成」。
   因此守护进程没有任何 capability，**无法创建 TUN 设备**（实测：`tun device error ...
   Operation not permitted`；同一二进制以 root 运行则 `tun device ready dev="etp0"`）。
2. **脚本环境**：`preinst` 阶段只有 `SYNOPKG_PKGDEST_VOL`，没有 `SYNOPKG_PKGVAR`/`SYNOPKG_PKGDEST`
   （因此 `preinst` 不能创建数据目录）；`start`/`stop`/`status` 阶段才会提供 `SYNOPKG_PKGVAR`。
3. **升级会跑 `preuninst`/`postuninst`**（`SYNOPKG_PKG_STATUS=UPGRADE`），清理逻辑必须只在
   `UNINSTALL` 时执行，否则升级会丢密钥与机器标识。
4. **应用页面路径**：DSM 把第三方应用窗口开到 `/3rdparty/<package>/index.html`，该路径默认没有
   nginx 路由，需要套件通过 `web-config` worker 提供（本仓库的 `spk/nginx/easytier-pro.conf`）。
5. **DSM 不对包内 CGI/静态文件做登录门禁**（无 Cookie 访问返回 200），鉴权必须由套件自己完成。
   `authenticate.cgi` 只认标准登录会话携带的 `id` Cookie；由安装向导自动登录创建的
   `_SSID`-only 会话会被判为未登录（重新登录即可恢复）。
6. **nginx 注入的 `ports` 声明**：`web-config` worker 的 `ports` 是给 nginx 预留端口用的，
   `enable`/`disable` 同时声明同一端口会导致 `Runtime port ... conflict for nginx` 并使套件启动失败
   （272）。本项目只注入 location，不声明端口。

### 虚拟网卡（B12/B13）：已实现的行为

DSM 不允许第三方套件以 root 运行，也无从获得 capability，因此守护进程在每次启动时检查运行时
二进制的文件能力（`security.capability` 中的 `CAP_NET_ADMIN`），并把结果同步到 Console：

1. **默认（未授权）**：守护进程把本机在 Console 上的节点设为「无 TUN 模式」
   （`PUT /api/v1/tenants/{ws}/nodes/{id}/config`，在节点 override 中写入 `no_tun: true`），
   本机作为中继/子网代理节点加入网络，但没有虚拟 IP。概览页显示「运行模式：中继模式（无虚拟 IP）」
   并给出一次性授权命令。命令按当前套件数据目录生成，界面提供「复制命令」按钮。
   DSM 的 `/tmp` 为 `noexec`，命令必须指向运行时目录。
2. **管理员授权后**：capability 在每次启动时重新检查；一旦检测到，守护进程会**自动删除**该
   `no_tun` override，恢复完整模式，TUN 设备出现在概览页与 `local-summary.interfaces`。
3. **重新下载运行时后**：新二进制会丢失文件能力，守护进程会自动重新写入 `no_tun`，无需手工判断。

**为什么必须写 Console，而不是启动参数**：`--no-tun` 加在 `easytier-core` 命令行上没有任何效果。
secure mode 下 core 自身不建网络（`crate_cli_network` 为 false），实例配置全部由 Console 下发；
下发的 `NetworkConfig` 经 `gen_config()` 从 `gen_default_flags()` 构造，`no_tun` 默认 false，
只有 Console 给出的值才会生效（`no_tun` 属于 Console 的受管字段）。
实测：以非 root 运行 core、不带 `--no-tun`，仅在 Console 上设置 `no_tun: true`，
core 常驻、不创建 TUN、并成功入网。

**注意**：写节点配置的 `PUT` 必须使用**每次唯一**的 `Idempotency-Key`。Console 会按
idempotency key 记录该节点的 reconfigure 操作，再次收到同一个 key 时直接返回已记录的操作、
**不应用新配置**。实测：同一 key 先写 `{"no_tun":true}` 再写 `{}`，第二次返回 HTTP 200 但
override 仍为 `{"no_tun":true}`；换一个新 key 才真正生效。
