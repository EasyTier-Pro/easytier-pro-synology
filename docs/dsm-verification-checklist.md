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
| B3 | DSM 主菜单出现「EasyTier Pro」，点击打开 | 进入 `3rdparty/easytier-pro/index.html`，页面显示概览 |
| B4 | 未登录 DSM 会话时访问 `/webman/3rdparty/easytier-pro/api/status`（无 Cookie 或非管理员账号） | HTTP 401（非管理员账号 403），返回 JSON `error: dsm_auth_required` / `dsm_auth_forbidden` |
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

## C. 已知未在无 DSM 环境验证的点

- B4/B5：`authenticate.cgi` 会话校验与 `administrators` 组成员判断只能在 DSM 上验证。
  若实测该 CGI 在非 CGI 进程中不回显用户名，按设计预案改为随包安装的小型 CGI 反代
  （见仓库计划文档），**不得**降级为无鉴权的 API。
- B9：DSM 开机自启依赖 `INFO` 的 `startstop_restart_services` 与 `precheckstartstop`。
- B10/B11：套件升级与卸载行为由 DSM 保证，`postuninst` 只负责清理数据目录。
