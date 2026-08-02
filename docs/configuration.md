# 配置与部署

[返回 README](../README.md) · [English](configuration.en.md)

这份文档收纳 README 中不适合展开太长的细节：Docker、Release 包、`server.yaml`、Agent 安装、认证、AI Provider、告警、计划任务、操作审计和 Token 模型。

## Docker 部署

默认 `docker-compose.yml` 使用 SQLite，并把数据库持久化到 `./data/mizupanel.db`。配置 AI Provider 后，同一目录还会保存加密主密钥 `./data/ai.key`。

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
| `MIZUPANEL_DATA_DIR` | `./data` | SQLite 数据库和 AI 主密钥目录 |
| `MIZUPANEL_CONTAINER_NAME` | `mizupanel` | 容器名称 |
| `MIZUPANEL_AUDIT_RETENTION` | `90d` | 审计事件保留时间 |
| `MIZUPANEL_AUDIT_CLEANUP_INTERVAL` | `1h` | 审计过期清理间隔 |
| `MIZUPANEL_TASK_RETENTION` | `30d` | 任务执行历史保留时间 |
| `MIZUPANEL_TASK_CLEANUP_INTERVAL` | `1h` | 任务历史清理间隔 |

常用命令：

```bash
docker compose logs -f
docker compose down
```

备份或迁移 SQLite 部署时，必须把 `mizupanel.db` 和 `ai.key` 一起保存。数据库包含加密后的 Provider 凭据，但只有原 `ai.key` 可以解密；不要输出、复制到文档或单独公开该密钥内容。

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

AI 主密钥不存储在 MySQL 中。MizuPanel 容器仍将 `${MIZUPANEL_DATA_DIR:-./data}` 挂载到 `/app/data`，因此备份 MySQL 时也必须同时备份宿主机数据目录中的 `ai.key`。

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

audit:
  retention: "90d"
  cleanup_interval: "1h"

tasks:
  retention: "30d"
  cleanup_interval: "1h"

security:
  ai_key_file: "./data/ai.key"
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
| `audit.retention` | 操作审计事件保留时间，必须为正数时长 |
| `audit.cleanup_interval` | 审计过期清理间隔，必须为正数时长 |
| `tasks.retention` | 脚本和计划任务执行历史保留时间，必须为正数时长 |
| `tasks.cleanup_interval` | 任务历史过期清理间隔，必须为正数时长 |
| `security.ai_key_file` | AI Provider 凭据加密主密钥路径；必须与数据库一起备份 |
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

启用后，节点管理、系统设置、Agent 安装、计划任务、告警和 Kubernetes API 都需要登录。Agent WebSocket 连接不受 Dashboard 登录态影响。

## AI Provider 与本地数据

系统设置中的 **AI 模型配置** 支持保存多个 OpenAI Chat Completions 兼容 Provider。每条配置包含名称、Base URL、Model 和可选 API Key；只有聊天与 function tool calling 两项能力检测都通过后，才能设为默认或用于运维会话。Base URL 应包含兼容服务的 API 根路径，例如：

```text
http://model.internal:8000/v1
```

Server 会向规范化地址下的 `/chat/completions` 发起非流式请求。API Key 为空时不会发送 Bearer 凭据，适用于可信内网的无鉴权服务。保存后的 Key 不会回显；编辑时留空表示保留，勾选清除与输入替换值是两个独立操作。

```yaml
security:
  ai_key_file: "./data/ai.key"
```

也可通过 `MIZUPANEL_AI_KEY_FILE` 覆盖路径。首次需要保存 Provider 时，Server 会创建权限为 `0600` 的 32 字节主密钥；如果数据库已有加密凭据而密钥缺失、损坏或被替换，Server 会拒绝解密，不会静默生成新密钥覆盖问题。

会话、普通用户/助手消息、模型快照、轮次状态和规范化工具记录保存在现有数据库中。System prompt、原始模型请求/响应、隐藏推理、原始工具输出、日志正文和脚本正文不会作为会话历史持久化。模型调用是数据出站边界：只应配置你信任的内网或第三方模型服务。

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

## 计划任务与脚本库

Dashboard 的 **任务中心** 页面使用 `/api/automation` 下的认证接口管理脚本、计划和执行历史。计划表达式必须是标准五段 Cron，时区单独使用 IANA 名称（例如 `Asia/Shanghai` 或 `UTC`）；不支持秒字段、`@daily` 等宏、`CRON_TZ` 或 `TZ` 行。浏览器只展示 Server 返回的 UTC `next_run_at`，不会自行推导下次运行时间。

调度与脚本均保存在 Server 数据库中，不会导入、读取或修改 Agent 宿主机现有 `crontab`。Server 重启时，未完成的执行会标记为 `interrupted`；停机期间错过的多个周期最多合并补跑一次，不逐次回放。上一批相同计划仍运行时，新周期记录为 `skipped`，不会排队形成重叠执行。

执行边界：

- 脚本正文最大 128 KiB；Agent 以固定 `/bin/sh <私有临时脚本>` 运行，不接受解释器、工作目录、环境变量、参数、stdin 或 `sh -c` 命令字符串。
- 合并 stdout/stderr 最大保留 64 KiB，并在运行过程中截断；默认超时 300 秒，最大 1800 秒。
- 单批次最多 100 个节点；Server 全局最多并发 8 个目标、同一节点 1 个，Agent 最多同时执行 2 个任务。
- 目标状态区分成功、非零退出、超时、繁忙、取消、离线和旧 Agent 不支持。某个节点失败不会阻止同批次其他节点。
- 通知策略为 `never`、`failure` 或 `always`，复用现有 Webhook、钉钉和飞书配置；通知失败独立记录，不改变真实执行状态。

任务执行历史默认保留 30 天，并每小时删除过期批次和对应有限输出：

```yaml
tasks:
  retention: "30d"
  cleanup_interval: "1h"
```

环境变量覆盖：

```bash
MIZUPANEL_TASK_RETENTION=30d
MIZUPANEL_TASK_CLEANUP_INTERVAL=1h
```

两个值都必须是正数 Go 时长。脚本 CRUD、计划 CRUD/启停和手动执行会写入安全审计摘要；脚本正文、命令、输出、环境和通知 URL/密钥不会进入审计事件。

## 操作审计

Server 默认保留 90 天操作审计记录，并每小时删除一次过期记录：

```yaml
audit:
  retention: "90d"
  cleanup_interval: "1h"
```

也可以通过环境变量覆盖：

```bash
MIZUPANEL_AUDIT_RETENTION=90d
MIZUPANEL_AUDIT_CLEANUP_INTERVAL=1h
```

两个值都必须是 Go 时长格式的正数；配置无效时 Server 会拒绝启动。定时保留策略清理只删除超过保留期的事件，并继续保持不审计自身。

Dashboard 的 **审计日志** 页面使用只读接口 `GET /api/audit/events`。接口沿用管理员认证，支持 `before_id`、`limit`、`from`、`to`、`actor_type`、`actor_name`、`module`、`action`、`node_id`、`result` 和 `q` 参数；`limit` 最大为 100，分页使用返回的 `next_before_id`。

管理员也可以通过 `POST /api/audit/events/cleanup` 执行受控清理。接口沿用管理员认证，并要求请求同源、`Content-Type: application/json`；正文最多 4 KiB，只能包含一个严格 JSON 对象，未知字段、尾随 JSON 和同时或都不提供清理条件都会被拒绝。请求形状必须二选一：

```json
{ "older_than_days": 90 }
```

或：

```json
{ "before": "2026-06-01T00:00:00Z" }
```

`older_than_days` 必须是 1 到 3650 的整数，`before` 必须是 RFC3339 时间。Server 将条件标准化为 UTC 截止时间，只永久删除 `created_at < cutoff` 的记录，并拒绝任何会触及最近 24 小时或未来记录的截止时间。成功响应为 `{"deleted_count":12,"cutoff":"2026-06-01T00:00:00Z"}`。

该接口不提供全部删除、按 ID 删除或任意过滤条件删除。清理完成后会写入一条仍然保留的 `audit.cleanup` 事件，其中只包含标准化截止时间和删除数量，不保存请求正文、原始错误或其他秘密；后台定时保留策略清理仍不写审计事件。

审计事件只保存有界的操作类别、发起者类型、来源 IP、显式目标、结果、耗时、稳定摘要和白名单元数据。不会保存密码、Cookie、Token、Webhook URL、Compose YAML 或 `.env`、文件内容、终端命令/输出、Kubernetes Secret、原始请求/响应正文或远程诊断。写入失败不会替换原操作响应；该功能提供运维追溯证据，不是合规级不可变账本，也不提供多用户/RBAC 归属、导出、签名或 WORM 存储。

## Agent 安装

推荐从 Dashboard 点击 **添加服务器**，复制自动生成的 Linux 或 Windows 命令。Server 会为每次安装生成短期、仅用于首次引导的 `install_token`；当前有效期为 30 分钟，目标机器执行命令后会自动注册为节点。

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
| `install_token` | 短期（当前 30 分钟），绑定单个节点 | Dashboard 创建安装命令时由 Server 生成 | Agent 配置中短暂保存，换发后由 `node_token` 替换 | 首次引导；持久 Token 确认前可供同一节点恢复重试 |
| `node_token` | 长期，每个节点独立 | Server 在首次引导交换时生成 | Agent 本机配置文件；Server 端保存哈希 | Agent 注册完成后的重启和断线重连 |

注册流程：

```text
Dashboard 生成短期 install_token
        ↓
Agent 使用 install_token 发起首次注册
        ↓
Server 验证令牌并绑定 node.id
        ↓
Server 换发 node_token
        ↓
首次注册失败时，同一节点可在 TTL 内重试并取回同一个 node_token
        ↓
Agent 使用 node_token 重连
        ↓
node_token 成功认证后，临时引导令牌失效；后续继续使用 node_token
```

`install_token` 只能用于短期引导，不应作为持久凭据使用。它一旦绑定节点就拒绝其他节点复用，并在 TTL 到期时失效；`node_token` 在 Server 端只保存哈希，不保存明文。

## 调试日志

Server 和 Agent 都支持通过环境变量打开或关闭调试日志：

```bash
MIZUPANEL_DEBUG=true
```

生产环境建议保持关闭，只在排查 Agent 连接、指标上报、Kubernetes 代理或终端问题时临时启用。
