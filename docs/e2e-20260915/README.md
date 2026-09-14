# DSM 6 / DSM 7 全新安装与组网 E2E

测试日期：2026-09-15。测试对象为 `dist/` 中现有的两个 x86_64 SPK，
不是重新编译后的修复版本。源码基线：`692e39b`。

## 结论

**不能判定两个产物都可供新用户正常组网：DSM 7 通过默认无 TUN 模式的
新装、设备登录、入网和实际数据传输；DSM 6 被 DSM 会话校验问题阻断。**

| 检查 | DSM 6.2.4-25556 | DSM 7.4.1-90080 |
| --- | --- | --- |
| 卸载旧套件并验证数据已清空 | 通过 | 通过 |
| 套件中心手动安装当前 SPK | 通过 | 通过 |
| 安装后自动启动、主菜单入口 | 通过 | 通过 |
| 从主菜单打开套件、管理员鉴权 | **失败，且使 DSM 会话失效** | 通过 |
| 配置自建 Console、设备码登录 | 被鉴权问题阻断 | 通过 |
| 选择工作空间和注册密钥 | 未执行 | 通过 |
| 经界面下载安装稳定版核心 | 未执行 | 通过，2.6.4-8428a89d |
| 经界面加入网络 | 未执行 | 通过，10.166.77.1/24 |
| Linux 节点通过虚拟 IP 访问 NAS | 未执行 | 通过，8 MiB 文件校验一致 |
| Linux 节点 ping NAS 虚拟 IP | 未执行 | 3/3，0% 丢包 |
| 停止、启动套件后的自动恢复 | 未执行 | 再次下载校验一致，ping 3/3 |

DSM 7 未授予文件 capability，未使用 root 运行核心。默认无 TUN 模式允许
其它节点访问 NAS；NAS 自身主动访问其它虚拟 IP 的完整 TUN 模式不在此次通过范围内。

## 环境与产物

| 项目 | 值 |
| --- | --- |
| DSM 6 | PVE VM 142，DS918+，10.147.223.227 |
| DSM 7 | PVE VM 141，10.147.223.115；VM 名为 dsm72，实际系统为 7.4.1 |
| Console | 独立 Compose 项目 `dsm-e2e-20260915`，全新 PostgreSQL / Casdoor / receiver 卷 |
| Console 页面 | <http://web.10.147.223.128.nip.io:24173> |
| Console API | <http://console.10.147.223.128.nip.io:28080> |
| 配置服务器 | `tcp://10.147.223.128:32020` |
| 工作空间 | E2E User，`8255710a-281b-41f0-9d84-b808dcb944e6` |
| 网络 | DSM 新装组网验证，`10.166.77.0/24` |
| 网络 ID | `cb686ca4-c3b6-46bc-90f2-f6081bb725ad` |
| 对端 | 独立 Docker 容器 `dsm-e2e-20260915-peer`，10.166.77.2/24 |
| Console 后端镜像 | `sha256:695b7a649e2770ba37eeff178d4a8e3a0b1179d3a9b704f9c4b6d466650cdb93` |

Console 使用现有 E2E 镜像启动新实例，前端使用 dev 模式。保留了机器上原有的
`easytier-e2e-local` 环境。测试账号由 E2E 的 Casdoor 初始化流程预置到新数据库，
首次通过网页登录后创建空网络；本次不覆盖邮箱/短信注册。

产物均为 `1.0.0-0001`；完整摘要见 [SHA256SUMS](SHA256SUMS)。

- DSM 7：`easytier-pro-x86_64-1.0.0-0001.spk`
- DSM 6：`easytier-pro-x86_64-dsm6-1.0.0-0001.spk`

这是套件的全新安装测试，没有重新安装 DSM 操作系统。先用 `synopkg uninstall`
清理旧套件，确认包目录不存在、状态和运行时目录为空或不存在，再从浏览器的
套件中心上传 SPK，经过许可协议和安装确认，勾选安装后立即启动。

## 新用户流程截图

所有图片均来自实际浏览器操作；未制作模拟界面。DSM 授权截图中的设备码为
本次测试的短期验证码，报告不包含登录 Cookie、注册令牌或 Console 会话凭据。

| 步骤 | DSM 6 | DSM 7 |
| --- | --- | --- |
| 上传 SPK | [上传](dsm6-01-upload.png) | [上传](dsm7-01-upload.png) |
| 全新安装最终确认 | [确认](dsm6-02-install-confirm.png) | [确认](dsm7-02-install-confirm.png) |
| 安装后的 DSM 主菜单 | [入口](dsm6-03-menu-installed.png) | [入口](dsm7-03-menu-installed.png) |
| 首次打开 | [鉴权失败](dsm6-auth-session-failure.png) | [首次使用](dsm7-04-first-use.png) |
| 自建 Console 设置 | 未能完成 | [endpoint 设置](dsm7-05-console-endpoint.png) |
| 浏览器设备授权 | 未执行 | [授权页](dsm7-07-authorize.png) |
| 工作空间与密钥 | 未执行 | [选择密钥](dsm7-08-enrollment.png) |
| 安装运行时 | 未执行 | [安装入口](dsm7-09-runtime-install.png) |
| 入网与 peer 连接 | 未执行 | [成功入网](dsm7-12-peers-connected.png) |
| 套件重启后恢复 | 未执行 | [恢复状态](dsm7-13-after-restart.png) |

Console 侧：[空工作空间](console-02-empty-workspace.png)、
[新建网络](console-03-create-network.png)、
[DSM 与 Linux 对端同时在线](console-04-two-nodes-online.png)。

### DSM 7 操作过程

1. 从 DSM 主菜单打开套件，进入“首次使用”。
2. 在设置页填写测试 Console 和配置服务器地址，允许测试环境的 HTTP 地址。
3. 点击“开始使用”，在授权页面用测试账号完成设备码登录。
4. 回到套件后点击“继续设置”，选择工作空间的默认共享接入密钥。
5. 点击“安装并继续”，由套件下载并校验稳定版运行时。
6. 点击“加入 DSM 新装组网验证”。守护进程自动同步 `no_tun` 和
   `bind_device`，最终 `mode_synced=true`，未手工改写节点 override。
7. 用同一共享测试密钥启动独立 Linux 对端，在 Console 页面将它挂载到网络。
8. 从 Linux 对端访问 NAS 虚拟 IP 并验证数据。

独立 Console 初始未设置公开配置服务器和稳定下载版本，第一次激活报
`invalid_config_server`。补齐 Console 的公开地址及稳定版 `v2.6.4` 后重试成功。
另修正了独立栈 Access 容器的端口映射，使公开端口与容器实际监听的 21010 一致。
这些是本次测试环境的配置修正，没有修改 SPK 或核心，也没有作为产品失败计入。

## DSM 7 数据面证据

在 NAS 临时启动 HTTP 服务，提供 8 MiB 随机文件。从独立 Linux 容器执行：

```sh
wget -T 20 -O /tmp/from-dsm7.bin \
  http://10.166.77.1:18977/payload.bin
sha256sum /tmp/from-dsm7.bin
ping -c 3 -W 3 10.166.77.1
```

NAS 原文件与 Linux 收到文件的长度均为 `8388608` 字节，SHA-256 均为：

```text
3f4b44dc48994ddb4d367b1ad25379b2e61887513a74572099d2cbb1e13ff6d7
```

首次 ping：3 发 3 收，0% 丢包，平均 0.600 ms。
通过 `synopkg stop/start` 重启套件后，不重新登录、不重新加入网络，再次下载
8 MiB 文件，摘要相同；ping 仍为 3 发 3 收，平均 0.655 ms。
这里验证的是套件重启，没有覆盖 DSM 整机重启。

- [NAS 原文件摘要与真实 peer 表](dsm7-source-and-peers.txt)
- [重启后的下载及 ping 输出](dsm7-after-restart.txt)
- [最终状态：已登录、已同步、核心运行、无 TUN](dsm7-final.json)

CLI 显示 NAS 与 Linux 对端通过 TCP 直接连接。界面显示的 Peer 数为 4，
包含本机这一行，实际另外有 Linux 对端及两个平台组件；未将 4 解释成
4 个远端设备，也未将只有本机的一行误判为连接成功。

## DSM 6 阻断问题

### major，高置信度：套件鉴权使真实 DSM 管理员会话失效

在全新安装后，从 DSM 主菜单打开套件，页面提示“需要有效的 DSM 登录会话”。
重新登录及使用新的浏览器上下文仍能复现。为排除“没有成功登录”，在同一个
浏览器上下文、同一会话和 `X-Syno-Token` 下执行前后对照：

| 顺序 | 请求 | 结果 |
| --- | --- | --- |
| 1 | 直接调用 DSM `SYNO.Core.User` 的 `list` | `success: true` |
| 2 | 调用套件 `/3rdparty/easytier-pro/api/status` | HTTP 401，`dsm_auth_required` |
| 3 | 再次直接调用 DSM `SYNO.Core.User` 的 `list` | `success: false`，错误码 119 |

套件日志在第 2 步记录 DSM 错误码 **150**，随后会话持续失效。
原始对照数据见 [dsm6-session-probe.json](dsm6-session-probe.json)，
日志和进程身份见 [dsm6-runtime-evidence.txt](dsm6-runtime-evidence.txt)。

问题定位到 DSM 集成的鉴权边界：`internal/dsmenv/auth.go` 将浏览器 Cookie
和令牌转发到回环上的 DSM 管理 API；这次在 DSM 6 上的校验使原会话失效。
具体是 DSM 的哪项会话绑定检查触发，尚未进一步证明，因此不将 IP 或
User-Agent 某一项断言为已确认根因。

这是局部平台鉴权设计的兼容性问题，需要验证一种能保留 DSM 6 会话约束的
管理员校验方式后再修改。此次没有放宽鉴权、关闭 DSM 安全设置或用开发模式
绕过，也没有将后台强行配好后的结果算作新用户通过。

### major，高置信度：DSM 6 运行身份与文档承诺不符

全新安装后实际守护进程的四个 UID 均为 `187812`，`CapEff=0`。
因此不能按 README 中“DSM 6 以 root 运行、TUN 开箱可用”的说明验收。
本次未越过鉴权阻断继续安装 DSM 6 核心，故没有声称已完成其 TUN 或数据面验证。

## 范围与保留现场

- 仅测试这两个 x86_64 产物及上述 DSM 版本；不外推到 ARM、DSM 7.0/7.2。
- 没有测试管理员 `setcap` 后的完整 TUN 模式、子网路由、中继吞吐或整机重启。
- 没有修改产品代码。新增内容仅为本报告、截图和脱敏测试证据。
- 保留独立 Console、Linux 对端及 DSM 套件，便于继续检查；临时随机文件 HTTP
  服务在测试结束后停止。DSM 7 保持默认无 TUN 模式，不留额外 capability。
- 独立环境 Compose 文件位于 `/tmp/dsm-e2e-20260915/compose.json`，其中包含测试
  环境凭据，未提交。清理时只对这个 Compose 项目执行 `down -v`，并单独删除
  `dsm-e2e-20260915-peer`，不要清理原有 `easytier-e2e-local` 环境。
