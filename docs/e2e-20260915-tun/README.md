# DSM6 / DSM7 完整 TUN E2E

日期：2026-09-15。最终安装包：`1.0.0-0004`。

## 最终结论

**DSM6 默认 TUN、DSM7 授权后的 TUN 均通过真实 E2E，最终 0004 产物整机重启后自动恢复。**

| 检查 | DSM6 | DSM7 |
|---|---|---|
| 运行身份 | root | 套件用户 + NET_ADMIN/NET_RAW |
| 网卡及虚拟 IP | tun0 / 10.166.77.3 | tun0 / 10.166.77.1 |
| 整机重启后 | 自动启动、加载驱动并建网卡 | 自动启动、capability 保留并建网卡 |
| 主动从 Linux 下载 8 MiB | 摘要一致 | 摘要一致 |
| 重启后从另一台 NAS 下载 8 MiB | 摘要一致 | 摘要一致 |
| 重启后主动 ping 另一台 NAS | 5/5，零丢包 | 5/5，零丢包 |

重启前后 boot ID 不同，证据包含内核网卡、运行 UID、capability、文件大小和摘要：

- [DSM6 整机重启后](dsm6-after-reboot.txt)
- [DSM7 整机重启后](dsm7-after-reboot.txt)
- [DSM6 主动访问 DSM7](dsm6-to-other-nas.txt)
- [DSM7 主动访问 DSM6](dsm7-to-other-nas.txt)
- [最终 peer 表](peers-final.txt)
- [DSM6 重启后截图](tun-dsm6-10-after-reboot.png)
- [DSM7 重启后截图](tun-dsm7-02-after-reboot.png)
- [Console 三台设备运行中](tun-console-01-final.png)

整机重启期间，测试 VM 的 RR 引导器曾临时占用 DSM7 的 IP 并提供不同 SSH 主机密钥。
通过可信 PVE 的 QMP 截图确认是引导阶段；没有接受新密钥，DSM 启动后原 SSH 密钥恢复。
首次重启恢复时出现过约 1 秒的延迟峰值；本轮验证连通性与数据完整性，不作为性能基准。
临时 HTTP 服务及文件在验证后清理，Console、Linux peer、两台 NAS 的 TUN 网络保留。

## 修复内容

这次验证补齐了此前仅覆盖默认无 TUN 模式的缺口。

1. **DSM6 提前降权启动**：`precheckstartstop=yes` 让 DSM 先以套件用户执行
   `prestart`，再以 root 执行 `start`。旧脚本把两者都映射到启动函数，实际进程
   在预检查阶段已被启动。现在预检查只检查可执行文件，正式动作才操作进程。
   DSM6 的 `status` 也使用 root，避免 `kill -0` 无权检查 root daemon。
2. **DSM7 授权在启动时丢失**：旧脚本每次启动递归 `chown` 数据目录，清除了
   运行时的文件 capability。实机复现：`setcap` 后停止套件仍有 capability，
   启动后消失；以套件用户对该文件执行同属主 `chown` 同样会清除它。
   删除这段多余的属主修改，目录所有权继续由 DSM 安装流程管理。
3. **DSM6 整机重启缺少驱动**：当前内核未自动加载 `tun.ko`。首次 TUN 测试
   使用了环境中已加载的驱动，整机重启暴露此问题。root 启动时检查 TUN 驱动，
   未注册则加载 `/lib/modules/tun.ko`，缺少设备节点则创建 `/dev/net/tun`。

这些均为套件生命周期的局部实现问题。没有新增常驻权限服务，也没有手工替换 daemon。
DSM7 继续使用套件用户，只按界面命令授予运行时所需能力。

生命周期语义参考 [Synology 脚本文档](https://help.synology.com/developer-guide/synology_package/scripts.html)
和 [权限配置文档](https://help.synology.com/developer-guide/privilege/privilege_config.html)，
实际调用身份通过 DSM6 启停时临时记录 `id` 与动作确认；诊断记录代码已恢复。

## 环境与执行方式

- 独立 Console：`dsm-e2e-20260915`，沿用[前一轮测试环境](../e2e-20260915-fix/README.md)。
- DSM6：VM142，DSM 6.2.4-25556，10.147.223.227，虚拟 IP 10.166.77.3。
- DSM7：VM141，DSM 7.4.1-90080，10.147.223.115，虚拟 IP 10.166.77.1。
- Linux 对端：10.166.77.2；运行时版本 2.6.4-8428a89d。
- DSM6 先卸载并确认包目录及数据目录删除，移除 Console 中旧测试设备释放配额，
  再通过 DSM 套件中心全新安装 `0003`，走完 endpoint 设置、设备授权、注册、
  运行时下载和加入网络。首次进入完整模式无需管理员补权限。
- 首轮整机重启发现 DSM6 驱动未自动加载后，构建 `0004`，两台安装最终产物，
  再进行整机重启回归。DSM6 命令行升级仅完成安装，需要先通过 `synopkg start`
  启用套件，再验证开机自启。`0004` 的新增驱动初始化由此次冷启动覆盖；未重复全新注册。
- DSM7 升级修复包后，执行界面提供的命令并通过 DSM 套件管理重启：

```sh
sudo setcap cap_net_admin,cap_net_raw+ep /volume1/@appdata/easytier-pro/runtime/easytier-core
```

DSM7 在升级到最终 `0004` 时也保留了 capability。重新下载/替换运行时文件仍需重新授权，
本轮没有把这种替换行为与普通重启混为一谈。

## 已完成的首次 TUN 验证

两台均在概览页显示「完整模式（本机有虚拟网卡）」及 `tun0`。
DSM6 core 为 UID 0，DSM7 core 为套件 UID 187812，具有 NET_ADMIN / NET_RAW。

两台 NAS 分别通过虚拟 IP 主动下载 Linux 对端提供的 8 MiB 随机文件，
源文件和两个下载文件 SHA-256 一致：

```text
30552815cc326ee36653af83a65e42e83b777ac7dccc7219f5b26e61c614e580
```

DSM6 → DSM7、DSM7 → DSM6 的 ping 各 5/5，零丢包。
模式刚切换、节点尚未收敛时曾观察到短暂丢包；以上结果取连接稳定后的验证，
并非承诺启动过程中无瞬断。

- [DSM6 重启前证据](dsm6-before-reboot.txt)
- [DSM7 重启前证据](dsm7-before-reboot.txt)

## 新用户流程截图

| 阶段 | 截图 |
|---|---|
| DSM6 上传安装包 | [01](tun-dsm6-01-upload.png) |
| DSM6 安装确认 | [02](tun-dsm6-02-confirm.png) |
| 首次打开 | [03](tun-dsm6-03-first-use.png) |
| 自建 Console endpoint | [04](tun-dsm6-04-endpoint.png) |
| 设备授权码 | [05](tun-dsm6-05-device-code.png) |
| 授权成功 | [06](tun-dsm6-06-authorized.png) |
| 安装运行时 | [07](tun-dsm6-07-runtime.png) |
| 准备加入网络 | [08](tun-dsm6-08-ready.png) |
| DSM6 完整模式 | [09](tun-dsm6-09-full-mode.png) |
| DSM7 授权后的完整模式 | [DSM7](tun-dsm7-01-full-mode.png) |

## 产物与检查

- 最终产物在本机 `dist/easytier-pro-x86_64-dsm6-1.0.0-0004.spk`、
  `dist/easytier-pro-x86_64-1.0.0-0004.spk`。
- [安装包 SHA-256](SHA256SUMS)。dist 不入 Git。
- `sh scripts/test-lifecycle.sh` 验证预检查不创建运行状态及缺少 daemon 时拒绝启动。
- Shell 语法检查、前端构建、两个 SPK 结构检查通过。
