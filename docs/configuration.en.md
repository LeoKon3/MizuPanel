# Configuration And Deployment

[Back to README](../README.en.md) · [中文](configuration.md)

This page keeps the detailed setup notes out of the README: Docker, release packages, `server.yaml`, Agent installation, authentication, AI Providers, alerting, scheduled tasks, operational auditing, and token behavior.

## Docker With SQLite

Release images are published in the public Docker Hub repository `leokon3/mizupanel`. A target host needs only Docker Engine, Docker Compose, and a versioned Compose file—no MizuPanel source checkout, Go, Node.js, or local image build environment. The repository Compose file pins `leokon3/mizupanel:0.1.24` instead of following the moving `latest` tag.

First deployment:

```bash
mkdir -p mizupanel && cd mizupanel
curl -fLO https://raw.githubusercontent.com/LeoKon3/MizuPanel/v0.1.24/docker-compose.yml
docker compose pull
docker compose up -d
docker compose logs -f mizupanel
```

The panel binds to `127.0.0.1:8080` by default. To access it from the server IP or LAN, set the bind address and recreate the container:

```bash
MIZUPANEL_BIND_ADDR=0.0.0.0 docker compose up -d
```

The SQLite database persists at `${MIZUPANEL_DATA_DIR:-./data}/mizupanel.db`. After an AI Provider is configured, the same host directory also contains the encryption master key at `ai.key`.

Useful environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `MIZUPANEL_IMAGE` | `leokon3/mizupanel:0.1.24` | Complete image reference for a version, mirror, test image, or rollback |
| `MIZUPANEL_BIND_ADDR` | `127.0.0.1` | Docker port bind address |
| `MIZUPANEL_PORT` | `8080` | Host port |
| `MIZUPANEL_DATA_DIR` | `./data` | SQLite database and AI master-key directory |
| `MIZUPANEL_CONTAINER_NAME` | `mizupanel` | Container name |
| `MIZUPANEL_AUDIT_RETENTION` | `90d` | Audit-event retention |
| `MIZUPANEL_AUDIT_CLEANUP_INTERVAL` | `1h` | Expired-event cleanup interval |
| `MIZUPANEL_TASK_RETENTION` | `30d` | Task-run history retention |
| `MIZUPANEL_TASK_CLEANUP_INTERVAL` | `1h` | Expired task-run cleanup interval |

`MIZUPANEL_IMAGE` must be a complete image reference, such as `registry.example.com/mirror/mizupanel:0.1.24`. `latest` is for short-lived evaluation only; production, upgrades, and rollback should use an explicit SemVer tag.

Before every upgrade, back up the complete data directory so `mizupanel.db` and `ai.key` remain together:

```bash
docker compose stop mizupanel
tar -czf "mizupanel-sqlite-$(date +%Y%m%d-%H%M%S).tar.gz" "${MIZUPANEL_DATA_DIR:-./data}"
docker compose start mizupanel
```

Upgrading to a released version only pulls the image and recreates the container; it never builds locally. Persist the complete image reference in `.env` in the deployment directory so later plain Compose commands cannot fall back to the older default in the Compose file. This example uses the future target `0.1.25`, so replace it with the actual released version:

Set this in `.env`:

```dotenv
MIZUPANEL_IMAGE=leokon3/mizupanel:0.1.25
```

Keep any existing bind address and other settings in the file, then run:

```bash
chmod 600 .env
docker compose pull mizupanel
docker compose up -d
docker compose logs --tail=100 mizupanel
curl -fsS "http://127.0.0.1:${MIZUPANEL_PORT:-8080}/api/system/about"
```

If verification fails, change the same line in `.env` to the previous SemVer tag:

```dotenv
MIZUPANEL_IMAGE=leokon3/mizupanel:0.1.24
```

Then recreate the container. Restore the pre-upgrade backup if the data also needs to be rolled back:

```bash
docker compose pull mizupanel
docker compose up -d
docker compose logs --tail=100 mizupanel
```

Use `docker compose down` to stop while preserving data. Do not delete `${MIZUPANEL_DATA_DIR:-./data}` until a usable backup has been confirmed.

## Docker With MySQL

The MySQL deployment needs only the versioned `docker-compose.mysql.yml`. The MySQL Server configuration is included in the MizuPanel image, so `docker/server.mysql.yaml` no longer needs to be downloaded or mounted:

```bash
mkdir -p mizupanel && cd mizupanel
curl -fLO https://raw.githubusercontent.com/LeoKon3/MizuPanel/v0.1.24/docker-compose.mysql.yml
export MIZUPANEL_MYSQL_DATABASE=mizupanel
export MIZUPANEL_MYSQL_USERNAME=mizupanel
export MIZUPANEL_MYSQL_PASSWORD='change-this-password'
export MIZUPANEL_MYSQL_ROOT_PASSWORD='change-this-root-password'
docker compose -f docker-compose.mysql.yml pull
docker compose -f docker-compose.mysql.yml up -d
docker compose -f docker-compose.mysql.yml logs -f mizupanel
```

All four database variables must be available whenever Compose parses the file. Supply them through a protected deployment environment or a mode-`0600` `.env` file, and never commit that file. To expose the panel on the server IP or LAN:

```bash
MIZUPANEL_BIND_ADDR=0.0.0.0 docker compose -f docker-compose.mysql.yml up -d
```

MySQL data is stored in the `mizupanel_mizupanel-mysql-data` Docker volume. The AI master key is not stored in MySQL; the MizuPanel container still mounts `${MIZUPANEL_DATA_DIR:-./data}` at `/app/data`. Before upgrading, export MySQL and back up that host directory:

```bash
docker compose -f docker-compose.mysql.yml exec -T mysql \
  sh -c 'exec mysqldump --single-transaction -u"$MYSQL_USER" -p"$MYSQL_PASSWORD" "$MYSQL_DATABASE"' \
  > "mizupanel-mysql-$(date +%Y%m%d-%H%M%S).sql"
tar -czf "mizupanel-ai-key-$(date +%Y%m%d-%H%M%S).tar.gz" "${MIZUPANEL_DATA_DIR:-./data}"
```

Keep the MySQL volume in place when upgrading or rolling back the MizuPanel container. Change only `MIZUPANEL_IMAGE` in the protected `.env` and keep all four MySQL variables present:

For an upgrade, `.env` contains at least:

```dotenv
MIZUPANEL_IMAGE=leokon3/mizupanel:0.1.25
MIZUPANEL_MYSQL_DATABASE=mizupanel
MIZUPANEL_MYSQL_USERNAME=mizupanel
MIZUPANEL_MYSQL_PASSWORD=change-this-password
MIZUPANEL_MYSQL_ROOT_PASSWORD=change-this-root-password
```

Then run:

```bash
chmod 600 .env
docker compose -f docker-compose.mysql.yml pull mizupanel
docker compose -f docker-compose.mysql.yml up -d
```

For rollback, change only the image line in `.env` to:

```dotenv
MIZUPANEL_IMAGE=leokon3/mizupanel:0.1.24
```

Then run:

```bash
docker compose -f docker-compose.mysql.yml pull mizupanel
docker compose -f docker-compose.mysql.yml up -d
```

Use `docker compose -f docker-compose.mysql.yml down` to stop while preserving data. `docker compose -f docker-compose.mysql.yml down -v` deletes the MySQL volume and must only be used after verifying a backup and intentionally choosing to erase the database.

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
| `security.ai_key_file` | Master-key path for encrypted AI Provider credentials; back it up with the database |
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

## AI Providers And Local Data

The **AI Model Configuration** section in System Settings separates connections from models. Each OpenAI Chat Completions compatible Provider stores a name, Base URL, and optional API Key while owning multiple child models. After saving a connection, discover its bounded `/models` list and selectively import IDs, or add a model manually when the endpoint does not implement model discovery. Discovery does not automatically run capability calls for every result; only explicitly configured models are probed individually for chat and function-tool support. The Base URL should include the compatible API root, for example:

```text
http://model.internal:8000/v1
```

The Server sends non-streaming requests to `/chat/completions` under the normalized URL. An empty API Key omits Bearer authentication for trusted unauthenticated services on a private network. Saved keys are never echoed: leaving the edit field empty preserves the current key, while replacement and explicit clearing are separate actions. Changing the Base URL, protocol, or key invalidates connection discovery and every child-model capability result until they are tested again.

Only enabled models with verified chat and tool support can be selected. In a conversation, select a Provider first and then choose one of its models from the second menu; model names have no built-in operational roles. The global default only supplies the initial model for new conversations. Later choices are persisted per conversation and restored when older conversations are reopened, without rewriting historical Provider/model snapshots. A distinct global fallback model may also be configured. It is tried once only for a first-call timeout, rate limit, or upstream-availability failure before any tool call or result exists. Authentication, protocol, capability, cancellation, and post-tool failures never fall back. Completed messages identify the model that actually answered.

```yaml
security:
  ai_key_file: "./data/ai.key"
```

`MIZUPANEL_AI_KEY_FILE` can override the path. When a Provider is first saved, the Server creates a 32-byte master key with `0600` permissions. If encrypted credentials already exist and the key is missing, damaged, or replaced, the Server refuses decryption instead of silently creating a new key over the problem.

Conversations, ordinary user/assistant messages, requested/actual model snapshots, fallback state, turn status, and normalized tool records live in the existing database. System prompts, raw model requests/responses, hidden reasoning, raw tool output, log content, and script bodies are not persisted as conversation history. Model calls are a data-egress boundary, so configure only a trusted internal or third-party model service.

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

Both values must be positive Go duration strings; invalid values prevent the Server from starting. Scheduled retention cleanup removes only events outside the retention window and remains unaudited.

The Dashboard **Audit Trail** page uses the read-only `GET /api/audit/events` endpoint. It follows admin authentication and accepts `before_id`, `limit`, `from`, `to`, `actor_type`, `actor_name`, `module`, `action`, `node_id`, `result`, and `q`; `limit` is capped at 100 and pagination continues with the returned `next_before_id`.

Administrators can also use `POST /api/audit/events/cleanup` for controlled cleanup. The endpoint requires admin authentication and a same-origin request with `Content-Type: application/json`; its body is capped at 4 KiB and must contain one strict JSON object. Unknown fields, trailing JSON, and requests that provide both or neither cleanup condition are rejected. Exactly one of these request shapes is allowed:

```json
{ "older_than_days": 90 }
```

or:

```json
{ "before": "2026-06-01T00:00:00Z" }
```

`older_than_days` must be an integer from 1 through 3650, and `before` must be RFC3339. The Server normalizes the condition to a UTC cutoff, permanently deletes only rows where `created_at < cutoff`, and rejects any cutoff that would touch the latest 24 hours or future rows. A successful response is `{"deleted_count":12,"cutoff":"2026-06-01T00:00:00Z"}`.

There is no delete-all, per-ID deletion, or arbitrary-filter deletion API. After deletion, a surviving `audit.cleanup` event stores only the normalized cutoff and deleted count, never the request body, raw error, or another secret. Scheduled retention cleanup remains unaudited.

Events contain only bounded operation categories, actor types, source IPs, explicit targets, results, durations, stable summaries, and allowlisted metadata. They do not store passwords, cookies, tokens, webhook URLs, Compose YAML or `.env`, file contents, terminal commands/output, Kubernetes Secret data, raw request/response bodies, or remote diagnostics. A persistence failure does not replace the original operation response. This is operational evidence rather than a compliance-grade immutable ledger and does not add multi-user/RBAC attribution, export, signing, or WORM storage.

## Agent Install

The recommended path is to click **添加服务器** in the Dashboard and copy the generated Linux or Windows command. The Server generates a short-lived, bootstrap-only `install_token` for each command; its current TTL is 30 minutes.

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
| `install_token` | Short-lived (currently 30 minutes), bound to one node | Server when Dashboard creates an install command | Temporarily in Agent config, then replaced by `node_token` | Initial bootstrap; same-node recovery retries until the persistent token is confirmed |
| `node_token` | Long-lived per node | Server during the initial bootstrap exchange | Agent config file; hash on Server | Agent restarts and reconnects after registration |

Registration flow:

```text
Dashboard creates short-lived install_token
        ↓
Agent presents install_token for initial registration
        ↓
Server validates it and binds it to node.id
        ↓
Server issues node_token
        ↓
If registration fails, the same node can retry within the TTL and recover the same node_token
        ↓
Agent reconnects with node_token
        ↓
Successful node_token authentication retires the bootstrap token; later reconnects keep using node_token
```

`install_token` is bootstrap-only and must not be used as a persistent credential. Once bound, it cannot be reused by another node and it expires with its TTL. The Server stores only a hash of `node_token`, not the plaintext.
