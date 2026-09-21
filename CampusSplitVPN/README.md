# 校园 VPN 分流助手

目的是为了大家可以在家远程使用学校服务器（谁愿意天天憋在实验室呢）的同时使用codex

这是根据上级目录“网卡分发”说明制作的 macOS / Windows 小工具。它会自动完成三件事：

1. 找到校园 VPN 当前使用的隧道网卡；
2. 固定校园服务器或校园网段继续走该隧道；
3. 让普通网络流量恢复走本地网络，之后可再开启其他 VPN/代理。

## 使用

### 下载路径

进入仓库的 [`CampusSplitVPN/dist`](dist) 目录，根据系统下载：

- macOS（Intel / Apple 芯片，macOS 13+）：[`CampusSplitVPN/dist/校园VPN分流助手-macOS-universal.zip`](dist/校园VPN分流助手-macOS-universal.zip)
- Windows x64 压缩包：[`CampusSplitVPN/dist/校园VPN分流助手-Windows-x64.zip`](dist/校园VPN分流助手-Windows-x64.zip)
- Windows x64 单独程序：[`CampusSplitVPN/dist/校园VPN分流助手-Windows-x64.exe`](dist/校园VPN分流助手-Windows-x64.exe)

一般推荐下载对应系统的 ZIP。macOS 解压后得到 `校园VPN分流助手.app`；Windows 解压后得到 `校园VPN分流助手.exe`，不需要另外安装 Go、Python 或其他运行环境。

### 操作步骤

1. 先关闭其他 VPN/代理，只连接校园 VPN（MotionPro）。
2. 打开软件。macOS 双击 `校园VPN分流助手.app`；Windows 双击 `校园VPN分流助手.exe`。
3. 将默认的 `172.25.24.135` 改成使用者自己的校园服务器 IPv4 地址或网段；如果目标相同则无需修改。
4. 点击“一键分流”，确认 macOS 管理员密码框或 Windows UAC。
5. 显示成功后，再打开日常使用的其他 VPN/代理。

“恢复路由”会撤销本工具保存的临时改动。直接断开 VPN、重新联网或重启电脑通常也能恢复。若校园 VPN 客户端持续强制改写路由，可在软件中重新应用，但这类客户端可能无法稳定共存。

当前安装包未使用付费开发者证书。macOS 首次运行若拦截，打开设置-安全与隐私-最下面-仍要打开；Windows 若显示 SmartScreen，请先核对文件来源，再选择“更多信息 → 仍要运行”。macOS 通用版支持 Intel 与 Apple 芯片，最低系统版本为 macOS 13；Windows 版为 x64。

## 安全范围

- 地址输入只接受 IPv4 或 CIDR 网段，不执行用户输入的命令文本。
- 修改前保存原路由信息，Windows 路由写入 `ActiveStore`，不做永久路由。
- 控制页面只监听本机 `127.0.0.1`，并使用每次启动随机生成的访问令牌。
- 软件不会保存 VPN 密码，也不会连接任何外部服务。

## 命令行（可选）

```text
CampusSplitVPN --check --targets 172.25.24.135
CampusSplitVPN --apply --targets 172.25.24.135,172.25.0.0/16
CampusSplitVPN --restore
```

不带参数启动时会打开中文控制页面。
