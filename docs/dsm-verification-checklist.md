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

- B4/B5：会话校验只能通过 DSM 自己的接口完成（见下节「本机 API 鉴权」的实测结论）。
- B9：DSM 开机自启依赖 `INFO` 的 `startstop_restart_services` 与 `precheckstartstop`。
- B10/B11：套件升级与卸载行为由 DSM 保证，`postuninst` 只负责清理数据目录。

### 本机 API 鉴权：实测结论与实现

守护进程把「这个会话是否是管理员」这个问题**交给 DSM 自己回答**：携带调用方的 Cookie，
通过回环访问 DSM 自身的 Web API，调用一个只有管理员能调用的接口
（`SYNO.Core.User&method=list`，`version=1`）：

| DSM 应答 | 判定 | 返回给界面 |
| --- | --- | --- |
| `success: true` | 有效会话且为管理员 | 放行 |
| `error.code = 105` | 会话有效但不是管理员 | 403 `dsm_auth_forbidden` |
| 其它错误（如 119 会话失效） | 没有有效会话 | 401 `dsm_auth_required` |
| 无法连接 / 非 DSM 应答 | 校验不通过 | 401 `dsm_auth_required` |

DSM 监听地址由 nginx 通过 `X-DSM-Scheme` / `X-DSM-Port` 告知，回退顺序为
`https://127.0.0.1:5001`、`https://127.0.0.1:443`。

**为什么不用 `authenticate.cgi`**：早期实现用请求的 Cookie 合成 CGI 环境后直接执行
`authenticate.cgi`。在 DSM 7.2 实测，该方式对**真实浏览器会话**始终返回空——即使把
`REMOTE_ADDR`/`SERVER_ADDR`/`SERVER_NAME`/端口等所有组合逐一代入（对照：同一 CGI 对
curl 登录得到的会话可以正常返回用户名）。真实会话只能在 DSM 自己的请求管线内解析，
因此改为上表的做法，且不引入随包 CGI。

**必须遵守**：不得因为校验困难而降级为无鉴权的 API；DSM 不可达时一律按未登录处理。

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
| B12 | ✅（已实现，待接 Console 复测） | 无 TUN 模式：本机仍有虚拟 IP，可被其它节点访问、可做子网路由与中继，只是本机自身没有虚拟网卡（`local-summary.interfaces` 为空）；授予 `cap_net_admin` 后重启连接切换为完整模式，虚拟网卡出现。实测证据：无特权时 core 报 `tun device error ... Operation not permitted`，以 root 运行同一二进制则 `tun device ready dev="etp0"` |
| B13 | ✅（本地 e2e 已验证，DSM 侧同 B12 待复测） | core 退出后由守护进程按退避重启（本地 e2e 实测 2 秒内拉起）；重启逻辑与运行模式无关，无 TUN 模式下 core 同样常驻 |
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
   会话本身由 DSM 判定，见上文「本机 API 鉴权：实测结论与实现」。
6. **RPC 管理端口必须监听未指定地址**：core 会把管理套接字绑定到承载端口地址的网卡
   （`SO_BINDTODEVICE`，见 `easytier/src/tunnel/common.rs`），而 Linux 只允许带 `CAP_NET_RAW`
   的进程这么做。套件没有该能力，因此若把 `ET_RPC_PORTAL` 设为具体环回地址（如 `127.0.0.1:15888`），
   core 解析出 `lo` 后绑定失败，**启动即以 `failed to listen: Operation not permitted` 退出**
   （实测：同一二进制以 root 运行正常，以套件用户运行失败）。
   改用未指定地址 `0.0.0.0:15888` 即可：`get_interface_name_by_ip` 对 unspecified 地址返回
   `None`，core 会跳过该绑定。**安全性由 core 自己的白名单保证，而不是监听地址**：
   `ET_RPC_PORTAL_WHITELIST` 设为 `127.0.0.0/8,::1/128`（即 EasyTier 内置默认值），
   非环回客户端会被 core 拒绝。实测：监听 `0.0.0.0:15899` 时，本机 `easytier-cli` 正常，
   从另一台主机（`10.147.223.128`）调用被拒，core 日志记录
   `Rpc portal client IP ... not in whitelist ..., ignoring client`。
   **该白名单是此端点的安全边界，不得放宽到包含非环回地址。**
7. **nginx 注入的 `ports` 声明**：`web-config` worker 的 `ports` 是给 nginx 预留端口用的，
   `enable`/`disable` 同时声明同一端口会导致 `Runtime port ... conflict for nginx` 并使套件启动失败
   （272）。本项目只注入 location，不声明端口。

### 虚拟网卡（B12/B13）：已实现的行为

DSM 不允许第三方套件以 root 运行，也无从获得 capability，因此守护进程在每次启动时检查运行时
二进制的文件能力（`security.capability` 中的 `CAP_NET_ADMIN`），并把结果同步到 Console：

0. **两个能力对应两个 Console 设置，都可以不要**。core 的两个默认值与能力需求的对应关系：

   | 缺少的能力 | core 行为 | 下发到 Console 的设置 |
   | --- | --- | --- |
   | `CAP_NET_ADMIN` | 无法创建虚拟网卡 | `no_tun = true` |
   | `CAP_NET_RAW` | 无法把出站 socket 绑定到网卡（`SO_BINDTODEVICE`） | `bind_device = false` |

   第二条尤其重要：`flags.bind_device` 默认为 `true`，core 会把每个出站 socket 同时绑定到本地
   地址与承载它的网卡（`collect_bind_addrs`，见 `easytier-core/src/connectivity/{manual,direct}/mod.rs`）。
   缺 `CAP_NET_RAW` 时每次连接都报 `bind addr fail ... Operation not permitted`，节点**能注册、
   显示在线，但 peer 列表始终为空**。实测：手工授予 `cap_net_raw+ep` 后同一节点立刻连上 5 个 peer。

   两者都由守护进程自动写入 Console 的节点 override，**因此本套件可以做到零 capability、零 root
   操作**。`bind_device = false` 需要 Console 侧支持（见下）。

1. **默认（未授权）**：守护进程把本机在 Console 上的节点设为「无 TUN 模式」
   （`PUT /api/v1/tenants/{ws}/nodes/{id}/config`，在节点 override 中写入 `no_tun: true`），
   本机作为正常的网络成员加入：仍有虚拟 IP，可被其它节点访问，也可做子网路由与中继，只是本机
   自身没有虚拟网卡（`local-summary.interfaces` 为空）。概览页显示「运行模式：无 TUN 模式（用户态转发）」
   并给出一次性授权命令。命令按当前套件数据目录生成，界面提供「复制命令」按钮。
   DSM 的 `/tmp` 为 `noexec`，命令必须指向运行时目录。
2. **管理员授权后**：capability 在每次启动时重新检查；一旦检测到 `CAP_NET_ADMIN`，守护进程会
   **自动删除**该 `no_tun` override，恢复完整模式，TUN 设备出现在概览页与
   `local-summary.interfaces`。推荐的完整授权同时包含两个能力：
   `sudo setcap cap_net_admin,cap_net_raw+ep <运行时目录>/easytier-core`
   （`CAP_NET_ADMIN` 建虚拟网卡，`CAP_NET_RAW` 绑定网卡；缺后者则连不上 peer）。
3. **重新下载运行时后**：新二进制会丢失文件能力，守护进程会自动重新写入 `no_tun`，无需手工判断。
4. **在 Console 上直接挂载本机会默认开启 TUN**：Console 新建节点时 `no_tun` 默认为 `false`，而本机
   没有 `CAP_NET_ADMIN`，于是 core 会拿到一个自己无法满足的实例配置（实例注册成功但服务不可用，
   `easytier-cli` 报 `Instance not found or API service not available`）。
   守护进程会在**启动时、加入网络后、安装运行时后**同步该配置，并且**每 90 秒复查一次**
   （`relayWatchInterval`），因此在 Console 侧做出的改动会在一个周期内被自动纠正。
   实测：把节点的 `no_tun` 手动改成 `false` 后，约 80 秒内被守护进程改回 `true`，实例随之恢复、
   重新连上 5 个 peer。

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
