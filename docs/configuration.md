# 配置与部署

[返回 README](../README.md) · [English](configuration.en.md)

## 选择部署方式

| 方式 | 数据库 | 依赖 |
| --- | --- | --- |
| [Docker Compose（推荐）](#compose) | SQLite | Docker Engine、Compose 插件 |
| [docker run](#docker-run) | SQLite | Docker Engine |
| [Compose + MySQL](#mysql) | MySQL 8.4 | Docker Engine、Compose 插件 |
| [二进制发布包](#binary) | SQLite / MySQL | Linux amd64 或 arm64 |

以下镜像和下载命令使用 `0.1.34`。请确认该版本已经出现在 [Releases](https://github.com/LeoKon3/MizuPanel/releases) 中；源码分支版本可能先于正式发布。不要混用不同版本的安装包、Compose 文件和镜像。

Server 容器只运行面板，不会自动把宿主机加入监控。管理主机需要在目标主机上单独安装 Agent，无需给 Server 容器挂载 Docker socket。

<a id="compose"></a>
## Docker Compose（SQLite）

```bash
mkdir -p mizupanel && cd mizupanel
curl -fLO https://raw.githubusercontent.com/LeoKon3/MizuPanel/v0.1.34/docker-compose.yml
docker compose up -d
docker compose logs --tail=100 mizupanel
```

在 Docker 宿主机访问 **http://127.0.0.1:8080**。默认只开放本机端口，SQLite 和 AI 密钥持久化到当前目录的 `data/`。

检查运行版本：

```bash
curl -fsS http://127.0.0.1:8080/api/system/about
```

调整部署参数时，在部署目录创建 `.env`，以后始终从该目录运行 Compose：

```dotenv
MIZUPANEL_IMAGE=leokon3/mizupanel:0.1.34
MIZUPANEL_BIND_ADDR=127.0.0.1
MIZUPANEL_PORT=8080
MIZUPANEL_DATA_DIR=./data
MIZUPANEL_CONTAINER_NAME=mizupanel
```

不要把 `.env` 提交到仓库。需要远程访问时，先完成[认证与网络配置](#remote-access)，再更改绑定地址。

<a id="docker-run"></a>
## 单独使用 Docker

与 Compose 使用相同镜像和持久化目录：

```bash
mkdir -p mizupanel/data && cd mizupanel
docker run -d \
  --name mizupanel \
  --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  --mount type=bind,src="$(pwd)/data",dst=/app/data \
  leokon3/mizupanel:0.1.34

docker logs --tail=100 mizupanel
curl -fsS http://127.0.0.1:8080/api/system/about
```

停止并删除容器使用 `docker stop mizupanel && docker rm mizupanel`，宿主机 `data/` 保留。不要使用同名容器或占用同一端口同时运行这几种部署方式。

<a id="mysql"></a>
## Docker Compose（MySQL）

下载 MySQL 版 Compose 文件：

```bash
mkdir -p mizupanel && cd mizupanel
curl -fLO https://raw.githubusercontent.com/LeoKon3/MizuPanel/v0.1.34/docker-compose.mysql.yml
```

创建权限为 `0600` 的 `.env` 文件，替换下面两个密码后再启动：

```dotenv
MIZUPANEL_IMAGE=leokon3/mizupanel:0.1.34
MIZUPANEL_BIND_ADDR=127.0.0.1
MIZUPANEL_PORT=8080
MIZUPANEL_DATA_DIR=./data
MIZUPANEL_MYSQL_DATABASE=mizupanel
MIZUPANEL_MYSQL_USERNAME=mizupanel
MIZUPANEL_MYSQL_PASSWORD=replace-with-a-long-random-password
MIZUPANEL_MYSQL_ROOT_PASSWORD=replace-with-a-different-random-password
```

```bash
chmod 600 .env
docker compose -f docker-compose.mysql.yml up -d
docker compose -f docker-compose.mysql.yml ps
docker compose -f docker-compose.mysql.yml logs --tail=100 mizupanel
```

面板等待 MySQL 健康检查通过后启动；数据库不向宿主机发布端口。数据库位于 Compose 项目专属卷 `<项目名>_mizupanel-mysql-data`，`data/` 保存面板 AI 密钥。删除并重建面板容器不会删除这些数据。

数据库变量在每次解析 Compose 时都必须存在。已有 MySQL 数据卷初始化后，修改 `.env` 中的密码不会自动修改数据库用户密码，需要另行完成数据库凭据更新。

<a id="binary"></a>
## 二进制发布包

Server 提供 Linux amd64 和 arm64 包。选择与机器匹配的架构；不需要 Go、Node.js 或 Docker：

```bash
# x86_64 使用 amd64；aarch64 使用 arm64
ARCH=amd64
curl -fLO "https://github.com/LeoKon3/MizuPanel/releases/download/v0.1.34/mizupanel-linux-${ARCH}.tar.gz"
tar -xzf "mizupanel-linux-${ARCH}.tar.gz"
cd "mizupanel-linux-${ARCH}"
cp server.example.yaml server.yaml
```

首次启动前编辑 `server.yaml`。仅本机使用可将监听地址改为：

```yaml
server:
  listen: "127.0.0.1:8080"
  public_url: ""
  enable_terminal: true
```

保留文件中的其他配置，然后从解压目录运行：

```bash
./mizupanel-server -config server.yaml
```

在同机打开 http://127.0.0.1:8080。SQLite 默认位于 `./data/mizupanel.db`，相对数据路径以工作目录为准。用 systemd 托管时固定 `WorkingDirectory`，可参考包内的 `systemd/mizupanel-server.service`，并按实际安装路径和服务用户调整。

发布包包含 Server、前端、Agent 下载文件与安装脚本，必须保留完整目录结构。Server 使用 CGO/系统 C 库；如果系统提示缺少 `GLIBC_*`，请使用 Docker 镜像或在目标发行版构建，不要通过替换系统 libc 解决。Linux Agent 为静态链接，Windows Agent 作为下载文件提供；Windows 不运行这个 Linux Server 包。

查看版本与动态库要求：

```bash
./mizupanel-server --version
ldd ./mizupanel-server
```

<a id="build"></a>
## 从源码构建

需要 Go（版本见 `go.mod`）、Node.js 22、npm 和 C 编译器。先安装开发依赖：

```bash
npm ci --prefix web --include=dev
NODE_ENV=production make package-linux-amd64 AMD64_CC=gcc
```

产物为 `dist/mizupanel-linux-amd64.tar.gz`。在原生 arm64 机器上使用：

```bash
NODE_ENV=production make package-linux-arm64 ARM64_CC=gcc
```

在 amd64 上交叉构建 arm64 Server，需要 `aarch64-linux-gnu-gcc`（例如 Debian/Ubuntu 的 `gcc-aarch64-linux-gnu`）。不要并行运行两个打包目标，它们共享准备目录。

从源码构建容器镜像：

```bash
docker build -t mizupanel:local .
```

将前面的部署命令中的镜像改为 `mizupanel:local`，或在 Compose `.env` 设置 `MIZUPANEL_IMAGE=mizupanel:local`。

<a id="remote-access"></a>
## 远程访问与认证

Compose 默认绑定 `127.0.0.1`，适合本机使用或由同机反向代理转发。远程浏览器与 Agent 需要可达的 Server 地址；远端机器的 `127.0.0.1` 指向它自己，不能用于连接面板。

1. 配置管理员认证。
2. 使用支持 WebSocket 的 HTTPS 反向代理，或限定访问范围的可信内网。
3. 如需绑定宿主机网卡，设置 `MIZUPANEL_BIND_ADDR=0.0.0.0` 并配合防火墙；二进制修改 `server.listen`。
4. 需要明确生成的 Agent 地址时，在 Server 配置的 `server.public_url` 填写对 Agent 可达的 URL。留空时按请求地址推导。

**Compose 的 `.env` 不是容器的环境变量文件。** 仅把认证变量写入 `.env` 不会启用认证，还要用覆盖文件传入。创建 `compose.auth.yml`：

```yaml
services:
  mizupanel:
    environment:
      MIZUPANEL_AUTH_ENABLED: "true"
      MIZUPANEL_ADMIN_USERNAME: ${MIZUPANEL_ADMIN_USERNAME:-admin}
      MIZUPANEL_ADMIN_PASSWORD: ${MIZUPANEL_ADMIN_PASSWORD:?set MIZUPANEL_ADMIN_PASSWORD}
```

在受保护的 `.env` 中添加 `MIZUPANEL_ADMIN_PASSWORD`，不要使用示例密码。启动和之后的升级始终带上覆盖文件：

```bash
docker compose -f docker-compose.yml -f compose.auth.yml up -d
# MySQL 使用 -f docker-compose.mysql.yml -f compose.auth.yml
```

`docker run` 可通过权限为 `0600` 的 `auth.env` 配置同样三个变量，再在创建容器时加入 `--env-file ./auth.env`。二进制可使用 Server 配置中的 `security.admin`，或由服务管理器提供这些环境变量。密码属于部署环境，不要放进命令历史、镜像、Git 或公开日志。

自定义 Server 文件时，从仓库 `docker/server.sqlite.yaml` 或 `docker/server.mysql.yaml` 开始，保留 `/app/data` 路径及正确存储驱动。将其只读挂载到 `/app/custom.yaml`，并把容器 command 设置为：

```yaml
services:
  mizupanel:
    command: ["/app/mizupanel-server", "--config", "/app/custom.yaml"]
    volumes:
      - ./server.yaml:/app/custom.yaml:ro
```

设置正确的配置与认证后，再从 **主机 → 添加主机** 获取 Agent 安装命令。

<a id="backup"></a>
## 备份、升级与回滚

生产部署固定版本，不用 `latest` 作为回滚依据。升级前停止写入并备份数据；AI Provider 密钥 `ai.key` 必须和数据库一起保存。

SQLite Compose 示例（默认 `./data`）：

```bash
docker compose stop mizupanel
tar -czf "mizupanel-data-$(date +%Y%m%d-%H%M%S).tar.gz" data
docker compose start mizupanel
```

MySQL 示例：

```bash
docker compose -f docker-compose.mysql.yml stop mizupanel
docker compose -f docker-compose.mysql.yml exec -T mysql \
  sh -c 'MYSQL_PWD="$MYSQL_PASSWORD" exec mysqldump --single-transaction --no-tablespaces -u"$MYSQL_USER" "$MYSQL_DATABASE"' \
  > "mizupanel-db-$(date +%Y%m%d-%H%M%S).sql"
tar -czf "mizupanel-key-$(date +%Y%m%d-%H%M%S).tar.gz" data
docker compose -f docker-compose.mysql.yml start mizupanel
```

使用认证覆盖文件时，以上命令也必须带上对应 `-f` 参数；数据路径不是 `./data` 时替换为实际路径。

升级时在 `.env` 修改 `MIZUPANEL_IMAGE` 为已发布的新版本，然后拉取并重建：

```bash
docker compose pull mizupanel
docker compose up -d mizupanel
curl -fsS "http://127.0.0.1:${MIZUPANEL_PORT:-8080}/api/system/about"
```

MySQL 加上 `-f docker-compose.mysql.yml`；启用认证时在登录后的面板或容器日志中核验运行版本。回滚要使用升级前的镜像和匹配的数据备份，不能假定旧程序兼容已迁移的新数据库。

`docker compose down` 保留命名卷和宿主机数据目录。**`down -v` 会删除 MySQL 数据卷**，仅在确认备份且明确需要清空测试环境时使用。不要对已有部署运行全局 `docker system prune --volumes`。

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

默认 Dashboard 不需要登录。对外开放前应启用认证，并限制网络访问；Compose 的环境变量传递方式见[远程访问与认证](#remote-access)。

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

系统设置中的 **AI 模型配置** 把连接和模型分开管理：一条 OpenAI Chat Completions 兼容 Provider 保存名称、Base URL 和可选 API Key，并可拥有多个子模型。保存连接后可检测 `/models`，从有界结果中按需导入模型，也可在兼容服务未实现模型列表时手动添加。模型发现不会自动逐个发起能力调用；只有显式配置的模型才需要单独检测聊天与 function tool calling 能力。Base URL 应包含兼容服务的 API 根路径，例如：

```text
http://model.internal:8000/v1
```

Server 会向规范化地址下的 `/chat/completions` 发起非流式请求。API Key 为空时不会发送 Bearer 凭据，适用于可信内网的无鉴权服务。保存后的 Key 不会回显；编辑时留空表示保留，勾选清除与输入替换值是两个独立操作。修改 Base URL、协议或 Key 会使连接发现结果和所有子模型能力结果失效，需重新检测。

只有已启用且聊天、工具能力均验证通过的模型可用于会话。会话中先选择 Provider，再从第二个下拉框选择该 Provider 下的模型；模型名称没有“运维主模型”等固定角色。全局默认模型只负责为新会话提供初始选择，后续选择按会话持久化，重新打开旧会话时恢复，切换不会改写历史消息的 Provider/模型快照。还可配置一个不同的全局回退模型。回退仅在首次模型调用、任何工具调用或结果出现之前发生超时、限流或上游不可用时尝试一次；认证、协议、能力、取消及工具阶段后的失败不会回退。最终消息会明确显示实际使用的模型。

```yaml
security:
  ai_key_file: "./data/ai.key"
```

也可通过 `MIZUPANEL_AI_KEY_FILE` 覆盖路径。首次需要保存 Provider 时，Server 会创建权限为 `0600` 的 32 字节主密钥；如果数据库已有加密凭据而密钥缺失、损坏或被替换，Server 会拒绝解密，不会静默生成新密钥覆盖问题。

会话、普通用户/助手消息、请求/实际模型快照、回退状态、轮次状态和规范化工具记录保存在现有数据库中。System prompt、原始模型请求/响应、隐藏推理、原始工具输出、日志正文和脚本正文不会作为会话历史持久化。模型调用是数据出站边界：只应配置你信任的内网或第三方模型服务。

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

推荐从 **主机 → 添加主机**，复制自动生成的 Linux 或 Windows 命令。Server 会为每次安装生成短期、仅用于首次引导的 `install_token`；当前有效期为 30 分钟，目标机器执行命令后会自动注册为节点。

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
