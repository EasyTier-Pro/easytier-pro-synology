# DSM6 鉴权修复与 DSM6 / DSM7 E2E

日期：2026-09-15。产物版本：`1.0.0-0002`。

## 结论

**DSM6 全新安装的新用户流程通过，DSM7 修复版本升级回归通过，两台 NAS 均成功组网。**
两台都按套件默认权限运行，无手工替换 daemon、预装运行时或授予 capability。
默认无 TUN 模式：其他节点可以通过虚拟 IP 访问 NAS 服务；NAS 本机没有虚拟网卡，
不能据此宣称 NAS 可主动访问其他虚拟节点。授予能力后的 TUN 模式不在本轮验证范围。

DSM7 的全新安装、首次授权和运行时下载已在[首次 E2E](../e2e-20260915/README.md)完成；
本轮在该安装上升级到修复版本，验证配置保留、鉴权、组网及重启恢复。
DSM6 本轮先卸载，确认 `/var/packages/easytier-pro` 和
`/usr/syno/etc/packages/easytier-pro/var` 不存在，再通过套件中心手动安装。

## 根因与修复

DSM6 会话绑定来源 IP。此前 daemon 携带 Cookie 和 CSRF 令牌，通过回环调用 DSM
管理员 API，却没有保留浏览器 IP；DSM 返回错误 150，并使原浏览器会话失效（随后为 119）。
修复从套件 nginx 已覆盖的 `X-Real-IP` 获取合法 IP，转发为 DSM 回环请求的
`X-Forwarded-For`；鉴权缓存同时按 Cookie、CSRF 令牌和来源 IP 隔离。
仍由 DSM 自身的管理员 API 决定权限，没有跳过鉴权，也不采用浏览器提供的转发链。
这是鉴权请求上下文丢失的局部实现问题。

## 测试环境

| 项目 | 环境 |
|---|---|
| DSM6 | VM 142，DSM 6.2.4-25556，10.147.223.227 |
| DSM7 | VM 141，DSM 7.4.1-90080，10.147.223.115 |
| Console | 独立 Docker Compose 项目 `dsm-e2e-20260915` |
| Console UI | http://web.10.147.223.128.nip.io:24173 |
| Console API | http://console.10.147.223.128.nip.io:28080 |
| 配置服务器 | tcp://10.147.223.128:32020 |
| 测试网络 | DSM 新装组网验证，10.166.77.0/24 |
| 测试节点 | Linux 10.166.77.2；DSM7 10.166.77.1；DSM6 10.166.77.3 |
| 运行时 | easytier-core / easytier-cli 2.6.4-8428a89d |

独立 Console 使用前一轮新建的测试账号、工作空间及网络；未接入生产 Console。
最终 Console 显示三个网络设备运行中，Linux peer 表中两台 DSM 均为 P2P。
测试结束保留 Console、Linux peer 和已连接的 DSM，便于检查；仅移除临时 HTTP 文件服务。

## 验证结果

| 检查 | DSM6 | DSM7 |
|---|---|---|
| 包安装 | 卸载及数据清理后全新安装 | 从 0001 升级到 0002 |
| Console endpoint | UI 设置并保存 | 升级保留 |
| 新用户授权 | device code → 浏览器授权 → 注册 | 前一轮完成，本轮保留 |
| 运行时 | UI 下载并安装 | 升级保留 |
| 加入网络 | UI 加入，10.166.77.3 | 自动恢复，10.166.77.1 |
| 管理员 API | 两个路径均 200 | 两个路径均 200 |
| 匿名 API | 401 | 401 |
| 原 DSM 会话 | 完整流程及重启后仍有效 | 升级及重启后仍有效 |
| 8 MiB HTTP 下载 | 重启前后 SHA-256 均与源文件一致 | 重启前后 SHA-256 均与源文件一致 |
| ping | 重启前后均 3/3，0% 丢包 | 重启前后均 3/3，0% 丢包 |
| 套件 stop / start | 无需重新授权、下载或加入 | 无需重新授权、下载或加入 |

鉴权覆盖 `/3rdparty/easytier-pro/api/status` 和
`/webman/3rdparty/easytier-pro/api/status`。真实浏览器带伪造的 `X-Real-IP`
及 `X-Forwarded-For` 仍由 nginx 覆盖为真实来源；持续操作超过 30 秒缓存 TTL 后会话有效。
非管理员拒绝、不同 IP 的缓存隔离、IPv4/IPv6 转发及非法 IP 拒绝由自动化测试覆盖；
本轮未新建 DSM 非管理员账号做实机验证。

文件由 NAS 上临时 HTTP 服务提供，Linux peer 通过虚拟 IP 下载随机 8 MiB 内容，
比较源文件和下载文件 SHA-256。测试 HTTP 服务以管理员启动仅用于提供测试文件；
套件与 core 的 UID 为 187812，CapEff 为 0，见原始证据。

## 截图

### DSM6 全新安装流程

| 步骤 | 截图 |
|---|---|
| 上传修复包 | [01](fix-dsm6-01-upload.png) |
| 安装确认 | [02](fix-dsm6-02-install-confirm.png) |
| 首次打开 | [03](fix-dsm6-03-first-use.png) |
| 自建 Console endpoint | [04](fix-dsm6-04-endpoint.png) |
| 获取授权码 | [05](fix-dsm6-05-device-code.png) |
| 浏览器授权成功 | [06](fix-dsm6-06-authorized.png) |
| 准备安装运行时 | [07](fix-dsm6-07-runtime-install.png) |
| 运行时完成，准备加入 | [08](fix-dsm6-08-ready.png) |
| 加入网络 | [09](fix-dsm6-09-connected.png) |
| 重启后恢复 | [10](fix-dsm6-10-restarted.png) |

### DSM7 回归与 Console

- [DSM7 上传修复包](fix-dsm7-01-upload.png)
- [DSM7 升级确认](fix-dsm7-02-upgrade.png)
- [DSM7 升级后组网](fix-dsm7-03-connected.png)
- [DSM7 重启恢复](fix-dsm7-04-restarted.png)
- [Console 三台设备运行中](fix-console-01-three-nodes.png)

## 原始证据及产物

- [鉴权和原 DSM 会话状态](auth-session.json)
- [Linux peer 表](peers.txt)
- [DSM6 重启后传输及 ping](dsm6-after-restart.txt)
- [DSM7 重启后传输及 ping](dsm7-after-restart.txt)
- [DSM6 版本、运行身份及源文件摘要](dsm6-runtime.txt)
- [DSM7 版本、运行身份及源文件摘要](dsm7-runtime.txt)
- [SPK SHA-256](SHA256SUMS)

安装包在本机 `dist/easytier-pro-x86_64-dsm6-1.0.0-0002.spk` 和
`dist/easytier-pro-x86_64-1.0.0-0002.spk`（dist 不入 Git）。

自动化验证：新回归用例先在旧代码失败，修复后 `go test -race ./internal/dsmenv`、
`go test ./...`、`go vet ./...` 通过；前端构建及两个 SPK 结构检查通过。

## 后续 TUN 验证

默认无 TUN 是当时启动脚本缺陷造成的 DSM6 实测结果，不代表 DSM6 平台只能无 TUN。后续修复与完整 TUN 结果见 [TUN E2E 报告](../e2e-20260915-tun/README.md)。
