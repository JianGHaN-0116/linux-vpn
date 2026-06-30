# vpn — mihomo 代理 CLI 工具

基于 [mihomo](https://github.com/MetaCubeX/mihomo)（Clash Meta）内核的 Linux 命令行代理管理工具。

[![Go Version](https://img.shields.io/badge/Go-1.23%2B-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-green)](LICENSE)

## 功能

- **订阅管理** — 添加/删除/切换/更新 clash 订阅链接（自动识别 base64 / YAML）
- **一键启停** — `vpn on` 启动 mihomo + 设置终端代理 + 配置 git 代理
- **规则代理** — 支持完整的 clash 分流规则、代理组、节点选择
- **TUN 模式** — 全局 VPN（需要 root）
- **Git 加速** — 自动配置 `git http.proxy`，HTTPS clone 走代理
- **纯 Go** — 单文件二进制，无运行时依赖

## 一键安装

```bash
curl -fsSL https://raw.githubusercontent.com/JianGHaN-0116/linux-vpn/main/install.sh | bash
```

安装脚本会自动处理：
- Go 编译器（如未安装）
- mihomo 内核（下载最新版）
- yq（YAML 处理工具）
- GeoIP / GeoSite 数据
- 编译并安装 `vpn` 到 `/usr/local/bin`
- Shell 集成（bash / zsh / fish）

### 手动安装

```bash
git clone https://github.com/JianGHaN-0116/linux-vpn.git
cd vpn
sudo ./install.sh
```

## 用法

```bash
# 添加订阅
vpn sub add https://your-subscription-url

# 查看订阅列表
vpn sub list

# 切换订阅
vpn sub use 1

# 开启代理
vpn on

# 查看状态
vpn status

# 查看日志
vpn log -f

# 关闭代理
vpn off

# 更新订阅
vpn sub update
```

### Git 加速

`vpn on` 自动执行 `git config --global http.proxy http://127.0.0.1:7890`。
如果你的 GitHub 远程是 **SSH 协议** (`git@github.com`)，需要额外配置：

```bash
# 方案 1：改用 HTTPS 远程
git remote set-url origin https://github.com/user/repo.git

# 方案 2：SSH 走代理（需要 socat）
vpn git-ssh on    # 写入 ~/.ssh/config
vpn git-ssh off   # 还原
```

### 环境变量

```bash
# 在当前 shell 中设置代理
eval "$(vpn on -e)"

# 取消
vpn off -e
```

## 命令参考

| 命令 | 说明 |
|------|------|
| `vpn on` | 启动 mihomo + 设置 git 代理 + 打印 shell 环境变量 |
| `vpn off` | 停止 mihomo + 清除代理 |
| `vpn restart` | 重启 mihomo |
| `vpn status` | 查看运行状态、节点、代理设置 |
| `vpn log [-f]` | 查看 / 跟踪 mihomo 日志 |
| `vpn sub add <url>` | 添加订阅 |
| `vpn sub list` | 列出所有订阅 |
| `vpn sub use <id>` | 切换订阅 |
| `vpn sub update [id]` | 更新订阅 |
| `vpn sub remove <id>` | 删除订阅 |
| `vpn config show` | 查看配置 |
| `vpn tun on/off` | TUN 模式（需 root） |
| `vpn import` | 从 clashctl 迁移 |

## 项目结构

```
vpn/
├── main.go                    # CLI 入口
├── install.sh                 # 一键安装脚本
├── internal/
│   ├── config/config.go       # 配置管理
│   ├── kernel/kernel.go       # mihomo 进程管理
│   ├── sub/sub.go             # 订阅下载 / 合并
│   └── proxy/proxy.go         # 系统代理 / git 代理
└── go.mod
```

## 依赖

- **mihomo** — Clash Meta 内核（自动下载）
- **yq** — YAML 处理（自动下载，v4.44+）
- **Go 1.23+** — 仅编译时需要

## License

MIT © 2025
