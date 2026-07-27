# Configuration And Deployment

[Back to README](../README.en.md) · [中文](configuration.md)

This page keeps the detailed setup notes out of the README: Docker, release packages, `server.yaml`, Agent installation, authentication, alerting, scheduled tasks, operational auditing, and token behavior.

## Docker

The default `docker-compose.yml` uses SQLite and persists data to `./data/mizupanel.db`.

```bash
docker compose up -d
```

By default, the panel binds to `127.0.0.1:8080`. To access it from the server IP or LAN:

```bash
MIZUPANEL_BIND_ADDR=0.0.0.0 docker compose up -d
```

Useful environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `MIZUPANEL_BIND_ADDR` | `127.0.0.1` | Docker port bind address |
| `MIZUPANEL_PORT` | `8080` | Host port |
| `MIZUPANEL_DATA_DIR` | `./data` | SQLite data directory |
| `MIZUPANEL_CONTAINER_NAME` | `mizupanel` | Container name |
| `MIZUPANEL_AUDIT_RETENTION` | `90d` | Audit-event retention |
| `MIZUPANEL_AUDIT_CLEANUP_INTERVAL` | `1h` | Expired-event cleanup interval |
| `MIZUPANEL_TASK_RETENTION` | `30d` | Task-run history retention |
| `MIZUPANEL_TASK_CLEANUP_INTERVAL` | `1h` | Expired task-run cleanup interval |

Useful commands:

```bash
docker compose logs -f
docker compose down
```

## Docker With MySQL

The MySQL setup uses `docker-compose.mysql.yml` and `docker/server.mysql.yaml`. Set credentials first:

```bash
export MIZUPANEL_MYSQL_DATABASE=mizupanel
export MIZUPANEL_MYSQL_USERNAME=mizupanel
export MIZUPANEL_MYSQL_PASSWORD='change-this-password'
export MIZUPANEL_MYSQL_ROOT_PASSWORD='change-this-root-password'
```

Start:

```bash
docker compose -f docker-compose.mysql.yml up -d
```

Expose it to the server IP or LAN if needed:

```bash
MIZUPANEL_BIND_ADDR=0.0.0.0 docker compose -f docker-compose.mysql.yml up -d
```

MySQL data is stored in the Docker volume:

```text
mizupanel_mizupanel-mysql-data
```

Stop while keeping data:

```bash
docker compose -f docker-compose.mysql.yml down
```

Stop and delete MySQL data:

```bash
docker compose -f docker-compose.mysql.yml down -v
```

## Release Package

Build the package for the Server architecture:

```bash
make package-linux-amd64
make package-linux-arm64
```

Generated output:

```text
dist/
├── mizupanel-linux-amd64/
├── mizupanel-linux-amd64.tar.gz
├── mizupanel-linux-arm64/
└── mizupanel-linux-arm64.tar.gz
```

Deploy:

```bash
tar -xzf dist/mizupanel-linux-amd64.tar.gz
cd mizupanel-linux-amd64
cp server.example.yaml server.yaml
./mizupanel-server -config server.yaml
```

The arm64 Server package needs an arm64 C cross compiler because the Server uses CGO SQLite:

```bash
sudo apt install gcc-aarch64-linux-gnu
```

## Server Config

Template: [examples/server.example.yaml](../examples/server.example.yaml)

```yaml
server:
  listen: ":8080"
  public_url: ""
  enable_terminal: true

storage:
  driver: "sqlite"
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

Important fields:

| Field | Description |
| --- | --- |
| `server.listen` | HTTP listen address |
| `server.public_url` | Public panel URL used in Agent install commands |
| `server.enable_terminal` | Enables browser terminal routes |
| `storage.driver` | `sqlite` or `mysql` |
| `metrics.retention` | Historical metric retention |
| `audit.retention` | Operational audit-event retention; must be a positive duration |
| `audit.cleanup_interval` | Expired audit-event cleanup interval; must be a positive duration |
| `tasks.retention` | Script and scheduled-task run-history retention; must be a positive duration |
| `tasks.cleanup_interval` | Expired task-run cleanup interval; must be a positive duration |
| `security.admin.enabled` | Enables Dashboard admin login |
| `alerting.enabled` | Enables alert engine |
| `alerting.check_interval` | Alert rule check interval |

If Agents access the panel from other machines, set `public_url`:

```yaml
server:
  public_url: "http://your-server-ip:8080"
```

## Admin Authentication

The Dashboard is unauthenticated by default for trusted self-hosted usage. Enable admin authentication before exposing it beyond a trusted network:

```yaml
security:
  admin:
    enabled: true
    username: admin
    password: your-secret-password
    session_ttl: 24h
```

Environment overrides:

```bash
MIZUPANEL_AUTH_ENABLED=true
MIZUPANEL_ADMIN_USERNAME=admin
MIZUPANEL_ADMIN_PASSWORD=your-secret-password
MIZUPANEL_SESSION_TTL=24h
```

When enabled, node management, system settings, Agent installation, automation tasks, alerts, and Kubernetes APIs require login. Agent WebSocket connections are not affected by Dashboard sessions.

## Alerting

```yaml
alerting:
  enabled: true
  check_interval: "30s"
  max_rules: 100
```

Environment overrides:

```bash
MIZUPANEL_ALERTING_ENABLED=true
MIZUPANEL_ALERT_CHECK_INTERVAL=30s
```

Alert rules currently support CPU, memory, disk, swap, and system load metrics, comparison operators such as `>`, `>=`, `<`, `<=`, `=`, and duration-based conditions.

## Scheduled Tasks And Script Library

The Dashboard **Task Center** uses authenticated endpoints under `/api/automation` for scripts, schedules, and run history. Schedules accept standard five-field Cron expressions and a separate IANA time zone such as `Asia/Shanghai` or `UTC`. Seconds, macros such as `@daily`, and embedded `CRON_TZ` or `TZ` directives are rejected. The browser displays the UTC `next_run_at` computed by the Server and does not derive schedule times itself.

Scripts and schedules are persisted in the Server database. MizuPanel never imports, reads, or modifies an Agent host's existing `crontab`. Runs left incomplete by a Server restart become `interrupted`; multiple periods missed during downtime collapse into at most one catch-up run. If the previous batch for the same schedule is still active, the new occurrence is recorded as `skipped` instead of overlapping or silently queueing.

Execution boundaries:

- Script content is limited to 128 KiB. Agents use fixed `/bin/sh <private-temporary-script>` argv and do not accept an interpreter, working directory, environment, arguments, stdin, or a `sh -c` command string.
- Combined stdout/stderr is truncated during execution at 64 KiB. The default timeout is 300 seconds and the maximum is 1,800 seconds.
- A batch targets at most 100 nodes. The Server allows eight target executions globally and one per node; each Agent allows two concurrent tasks.
- Target results distinguish success, non-zero exit, timeout, busy, cancelled, offline, and unsupported older Agents. One failed node does not stop the other targets.
- Notification policies are `never`, `failure`, and `always`, reusing Webhook, DingTalk, and Feishu. Delivery failure is stored independently and never changes the execution result.

Run history is retained for 30 days by default, with expired batches and their bounded output removed hourly:

```yaml
tasks:
  retention: "30d"
  cleanup_interval: "1h"
```

Environment overrides:

```bash
MIZUPANEL_TASK_RETENTION=30d
MIZUPANEL_TASK_CLEANUP_INTERVAL=1h
```

Both values must be positive Go duration strings. Script CRUD, schedule CRUD/toggle, and manual execution write safe audit summaries; script bodies, commands, output, environment, notification URLs, and credentials never enter audit events.

## Audit Trail

The Server retains operational audit events for 90 days by default and removes expired rows hourly:

```yaml
audit:
  retention: "90d"
  cleanup_interval: "1h"
```

Environment overrides are also available:

```bash
MIZUPANEL_AUDIT_RETENTION=90d
MIZUPANEL_AUDIT_CLEANUP_INTERVAL=1h
```

Both values must be positive Go duration strings; invalid values prevent the Server from starting. Cleanup only removes events outside the retention window and does not audit itself.

The Dashboard **Audit Trail** page uses the read-only `GET /api/audit/events` endpoint. It follows admin authentication and accepts `before_id`, `limit`, `from`, `to`, `actor_type`, `actor_name`, `module`, `action`, `node_id`, `result`, and `q`; `limit` is capped at 100 and pagination continues with the returned `next_before_id`.

Events contain only bounded operation categories, actor types, source IPs, explicit targets, results, durations, stable summaries, and allowlisted metadata. They do not store passwords, cookies, tokens, webhook URLs, Compose YAML or `.env`, file contents, terminal commands/output, Kubernetes Secret data, raw request/response bodies, or remote diagnostics. A persistence failure does not replace the original operation response. This is operational evidence rather than a compliance-grade immutable ledger and does not add multi-user/RBAC attribution, export, signing, or WORM storage.

## Agent Install

The recommended path is to click **添加服务器** in the Dashboard and copy the generated Linux or Windows command. The Server generates a one-time `install_token` for each install command.

Linux example:

```bash
curl -fsSL 'http://your-panel-host:8080/scripts/install-agent.sh' -o install-agent.sh \
  && chmod +x install-agent.sh \
  && ./install-agent.sh \
    --binary-base-url 'http://your-panel-host:8080/downloads' \
    --server-url 'ws://your-panel-host:8080/api/agent/ws' \
    --token 'one-time-install-token' \
    --mode 'ops' \
    --node-id "$(hostname)" \
    --name "$(hostname)" \
    --enable-docker \
    --enable-terminal
```

Windows commands must run in administrator PowerShell. Prefer the command generated by the Dashboard.

Linux Agent default paths:

```text
/usr/local/mizupanel/mizupanel-agent
/usr/local/mizupanel/agent.yaml
/etc/systemd/system/mizupanel-agent.service
```

Inspect the service:

```bash
systemctl status mizupanel-agent
journalctl -u mizupanel-agent -f
```

Windows Agent default paths:

```text
C:\Program Files\MizuPanel\mizupanel-agent.exe
C:\Program Files\MizuPanel\agent.yaml
```

The Windows service name is `mizupanel-agent`.

## Token Model

| Token | Lifetime | Generated By | Stored In | Purpose |
| --- | --- | --- | --- | --- |
| `install_token` | One-time | Server when Dashboard creates an install command | Not persisted to Agent | First Agent registration |
| `node_token` | Long-lived per node | Server after successful first registration | Agent config file; hash on Server | Agent reconnects |

Registration flow:

```text
Dashboard creates install_token
        ↓
Agent registers for the first time
        ↓
Server validates install_token
        ↓
Server issues node_token
        ↓
Agent reconnects with node_token
```

`install_token` should not be used as a persistent credential. The Server stores only a hash of `node_token`, not the plaintext.
