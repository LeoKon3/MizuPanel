# 配置与部署

[返回 README](../README.md) · [English](configuration.en.md)

这份文档收纳 README 中不适合展开太长的细节：Docker、Release 包、`server.yaml`、Agent 安装、认证、告警和 Token 模型。

## Docker 部署

默认 `docker-compose.yml` 使用 SQLite，并把数据库持久化到 `./data/mizupanel.db`。

```bash
docker compose up -d
```

默认端口绑定为 `127.0.0.1:8080`。如果需要从服务器 IP 或局域网访问，显式设置绑定地址：

```bash
MIZUPANEL_BIND_ADDR=0.0.0.0 docker compose up -d
```

常用环境变量：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `MIZUPANEL_BIND_ADDR` | `127.0.0.1` | Docker 端口绑定地址 |
| `MIZUPANEL_PORT` | `8080` | 宿主机端口 |
| `MIZUPANEL_DATA_DIR` | `./data` | SQLite 数据目录 |
| `MIZUPANEL_CONTAINER_NAME` | `mizupanel` | 容器名称 |

常用命令：

```bash
docker compose logs -f
docker compose down
```

## Docker 使用 MySQL

MySQL 版本使用 `docker-compose.mysql.yml` 和 `docker/server.mysql.yaml`。启动前先设置数据库环境变量：

```bash
export MIZUPANEL_MYSQL_DATABASE=mizupanel
export MIZUPANEL_MYSQL_USERNAME=mizupanel
export MIZUPANEL_MYSQL_PASSWORD='换成你的数据库密码'
export MIZUPANEL_MYSQL_ROOT_PASSWORD='换成你的 Root 密码'
```

启动：

```bash
docker compose -f docker-compose.mysql.yml up -d
```

如果需要从服务器 IP 或局域网访问：

```bash
MIZUPANEL_BIND_ADDR=0.0.0.0 docker compose -f docker-compose.mysql.yml up -d
```

MySQL 数据保存在 Docker volume：

```text
mizupanel_mizupanel-mysql-data
```

停止但保留数据：

```bash
docker compose -f docker-compose.mysql.yml down
```

停止并删除 MySQL 数据：

```bash
docker compose -f docker-compose.mysql.yml down -v
```

## Release 包部署

按 Server 所在机器架构选择构建目标：

```bash
make package-linux-amd64
make package-linux-arm64
```

生成结果：

```text
dist/
├── mizupanel-linux-amd64/
├── mizupanel-linux-amd64.tar.gz
├── mizupanel-linux-arm64/
└── mizupanel-linux-arm64.tar.gz
```

解压后的包结构：

```text
mizupanel-linux-amd64/
├── mizupanel-server
├── server.example.yaml
├── data/
├── scripts/
│   ├── install-agent.sh
│   ├── install-agent.ps1
│   ├── uninstall-agent.sh
│   └── uninstall-agent.ps1
├── systemd/
│   ├── mizupanel-server.service
│   └── mizupanel-agent.service
├── downloads/
│   ├── mizupanel-agent-linux-amd64
│   ├── mizupanel-agent-linux-arm64
│   └── mizupanel-agent-windows-amd64.exe
└── web/
    ├── index.html
    └── assets/
```

部署步骤：

```bash
tar -xzf dist/mizupanel-linux-amd64.tar.gz
cd mizupanel-linux-amd64
cp server.example.yaml server.yaml
./mizupanel-server -config server.yaml
```

arm64 Server 包需要 arm64 C 交叉编译器，因为 Server 使用 CGO SQLite。Debian/Ubuntu 可安装：

```bash
sudo apt install gcc-aarch64-linux-gnu
```

## Server 配置

配置模板在 [examples/server.example.yaml](../examples/server.example.yaml)。

```yaml
server:
  listen: ":8080"
  public_url: ""
  enable_terminal: true

storage:
  driver: "sqlite"
  database_path: "./data/mizupanel.db"
  sqlite:
    path: "./data/mizupanel.db"
  mysql:
    host: "127.0.0.1"
    port: 3306
    username: "mizupanel"
    password: ""
    database: "mizupanel"

metrics:
  retention: "6h"
  cleanup_interval: "10m"

security:
  admin:
    enabled: false
    username: "admin"
    password: ""
    session_ttl: "24h"

alerting:
  enabled: true
  check_interval: "30s"
  max_rules: 100
```

关键字段：

| 字段 | 说明 |
| --- | --- |
| `server.listen` | Server HTTP 监听地址 |
| `server.public_url` | 生成 Agent 安装命令时使用的面板地址；留空时按请求 Host 推断 |
| `server.enable_terminal` | 是否启用浏览器终端路由 |
| `storage.driver` | `sqlite` 或 `mysql` |
| `metrics.retention` | 历史指标保留时间 |
| `security.admin.enabled` | 是否启用 Dashboard 管理员登录 |
| `alerting.enabled` | 是否启用告警引擎 |
| `alerting.check_interval` | 告警规则检查间隔 |

如果 Agent 从其他机器访问面板，建议设置 `public_url`：

```yaml
server:
  public_url: "http://你的服务器IP:8080"
```

## 管理员认证

默认 Dashboard 不需要登录，适合本机或可信内网使用。需要访问保护时启用：

```yaml
security:
  admin:
    enabled: true
    username: admin
    password: your-secret-password
    session_ttl: 24h
```

也可以通过环境变量覆盖：

```bash
MIZUPANEL_AUTH_ENABLED=true
MIZUPANEL_ADMIN_USERNAME=admin
MIZUPANEL_ADMIN_PASSWORD=your-secret-password
MIZUPANEL_SESSION_TTL=24h
```

启用后，节点管理、系统设置、Agent 安装、告警和 Kubernetes API 都需要登录。Agent WebSocket 连接不受 Dashboard 登录态影响。

## 告警配置

```yaml
alerting:
  enabled: true
  check_interval: "30s"
  max_rules: 100
```

可用环境变量：

```bash
MIZUPANEL_ALERTING_ENABLED=true
MIZUPANEL_ALERT_CHECK_INTERVAL=30s
```

当前告警规则支持 CPU、内存、磁盘、Swap、系统负载等指标，支持 `>`、`>=`、`<`、`<=`、`=` 等比较方式，也支持持续时间判断。

## Agent 安装

推荐从 Dashboard 点击 **添加服务器**，复制自动生成的 Linux 或 Windows 命令。Server 会为每次安装生成一次性 `install_token`，目标机器执行命令后会自动注册为节点。

Linux 命令示例：

```bash
curl -fsSL 'http://你的面板地址:8080/scripts/install-agent.sh' -o install-agent.sh \
  && chmod +x install-agent.sh \
  && ./install-agent.sh \
    --binary-base-url 'http://你的面板地址:8080/downloads' \
    --server-url 'ws://你的面板地址:8080/api/agent/ws' \
    --token 'one-time-install-token' \
    --mode 'ops' \
    --node-id "$(hostname)" \
    --name "$(hostname)" \
    --enable-docker \
    --enable-terminal
```

Windows 命令需要在管理员 PowerShell 中执行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -Command "`$ErrorActionPreference='Stop'; `$script = Join-Path `$env:TEMP ('mizupanel-install-' + [guid]::NewGuid().ToString() + '.ps1'); Invoke-WebRequest -Uri 'http://你的面板地址:8080/scripts/install-agent.ps1' -UseBasicParsing -OutFile `$script -ErrorAction Stop; & `$script `
    -BinaryBaseUrl 'http://你的面板地址:8080/downloads' `
    -ServerUrl 'ws://你的面板地址:8080/api/agent/ws' `
    -Token 'one-time-install-token' `
    -NodeId `$env:COMPUTERNAME `
    -Name `$env:COMPUTERNAME"
```

Linux Agent 默认安装到：

```text
/usr/local/mizupanel/mizupanel-agent
/usr/local/mizupanel/agent.yaml
/etc/systemd/system/mizupanel-agent.service
```

查看服务：

```bash
systemctl status mizupanel-agent
journalctl -u mizupanel-agent -f
```

Windows Agent 默认安装到：

```text
C:\Program Files\MizuPanel\mizupanel-agent.exe
C:\Program Files\MizuPanel\agent.yaml
```

并注册为 `mizupanel-agent` Windows Service。

## Agent 卸载

Linux：

```bash
curl -fsSL 'http://你的面板地址:8080/scripts/uninstall-agent.sh' -o uninstall-agent.sh \
  && chmod +x uninstall-agent.sh \
  && ./uninstall-agent.sh
```

Windows 管理员 PowerShell：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -Command "`$ErrorActionPreference='Stop'; `$script = Join-Path `$env:TEMP ('mizupanel-uninstall-' + [guid]::NewGuid().ToString() + '.ps1'); Invoke-WebRequest -Uri 'http://你的面板地址:8080/scripts/uninstall-agent.ps1' -UseBasicParsing -OutFile `$script -ErrorAction Stop; & `$script"
```

卸载会停止并删除 Agent 服务和安装目录，不会自动删除 Server 数据库中的节点记录和历史指标。

## Agent 配置

配置模板在 [examples/agent.example.yaml](../examples/agent.example.yaml)。

```yaml
server:
  url: "ws://你的面板地址:8080/api/agent/ws"
  token: "one-time-install-token"

node:
  id: "oracle-sg-01"
  name: "Oracle SG"

runtime:
  interval: "5s"
  mode: "ops"

features:
  docker: true
  terminal: true
```

字段说明：

| 字段 | 说明 |
| --- | --- |
| `server.url` | Server WebSocket 地址 |
| `server.token` | 首次注册时是 `install_token`，注册成功后会换成 `node_token` |
| `node.id` | 节点唯一 ID |
| `node.name` | Dashboard 展示名 |
| `runtime.interval` | 指标采集间隔 |
| `runtime.mode` | 运行模式，常用 `ops` |
| `features.docker` | 是否采集 Docker 容器信息并允许容器操作 |
| `features.terminal` | 是否启用浏览器终端 |

## Token 模型

| Token | 生命周期 | 谁生成 | 存放位置 | 用途 |
| --- | --- | --- | --- | --- |
| `install_token` | 一次性 | Dashboard 创建安装命令时由 Server 生成 | 不持久化给 Agent | Agent 首次注册 |
| `node_token` | 长期，每个节点独立 | Server 首次注册成功后换发 | Agent 本机配置文件；Server 端保存哈希 | Agent 重启和断线重连 |

注册流程：

```text
Dashboard 生成 install_token
        ↓
Agent 首次注册
        ↓
Server 验证 install_token
        ↓
Server 换发 node_token
        ↓
Agent 后续使用 node_token 重连
```

`install_token` 不应作为持久凭据使用；`node_token` 在 Server 端只保存哈希，不保存明文。

## 调试日志

Server 和 Agent 都支持通过环境变量打开或关闭调试日志：

```bash
MIZUPANEL_DEBUG=true
```

生产环境建议保持关闭，只在排查 Agent 连接、指标上报、Kubernetes 代理或终端问题时临时启用。
