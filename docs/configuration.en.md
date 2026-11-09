# Configuration and deployment

[Back to README](../README.en.md) · [中文](configuration.md)

## Choose a deployment method

| Method | Database | Requirements |
| --- | --- | --- |
| [Docker Compose (recommended)](#compose) | SQLite | Docker Engine and Compose plugin |
| [docker run](#docker-run) | SQLite | Docker Engine |
| [Compose with MySQL](#mysql) | MySQL 8.4 | Docker Engine and Compose plugin |
| [Binary package](#binary) | SQLite / MySQL | Linux amd64 or arm64 |

The commands below use `0.1.34`. Verify that the version is available in [Releases](https://github.com/LeoKon3/MizuPanel/releases); a source branch can be ahead of a published release. Keep packages, Compose files, and images on matching versions.

The Server container does not automatically monitor its host. Install an Agent on each managed host. The Server does not need the host Docker socket mounted into it.

<a id="compose"></a>
## Docker Compose with SQLite

```bash
mkdir -p mizupanel && cd mizupanel
curl -fLO https://raw.githubusercontent.com/LeoKon3/MizuPanel/v0.1.34/docker-compose.yml
docker compose up -d
docker compose logs --tail=100 mizupanel
```

Open **http://127.0.0.1:8080** on the Docker host. The default port is local-only. SQLite and the AI key persist in `data/` in the deployment directory.

Check the running version:

```bash
curl -fsS http://127.0.0.1:8080/api/system/about
```

To customize deployment settings, create `.env` in that directory and always run Compose from there:

```dotenv
MIZUPANEL_IMAGE=leokon3/mizupanel:0.1.34
MIZUPANEL_BIND_ADDR=127.0.0.1
MIZUPANEL_PORT=8080
MIZUPANEL_DATA_DIR=./data
MIZUPANEL_CONTAINER_NAME=mizupanel
```

Do not commit `.env`. Configure [authentication and remote access](#remote-access) before changing the bind address.

<a id="docker-run"></a>
## Docker without Compose

Use the same image and persistent data directory:

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

Use `docker stop mizupanel && docker rm mizupanel` to remove the container while retaining the host `data/` directory. Do not run multiple deployment methods with the same container name or port.

<a id="mysql"></a>
## Docker Compose with MySQL

Download the MySQL Compose file:

```bash
mkdir -p mizupanel && cd mizupanel
curl -fLO https://raw.githubusercontent.com/LeoKon3/MizuPanel/v0.1.34/docker-compose.mysql.yml
```

Create a mode-`0600` `.env` file and replace both passwords before starting:

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

The panel waits for MySQL to become healthy. MySQL is not published on a host port. The database lives in the project-specific `<project>_mizupanel-mysql-data` volume; `data/` stores the AI key. Recreating the panel container preserves both.

All database variables must remain available when Compose parses the file. After MySQL initializes its volume, changing passwords in `.env` does not update the database accounts; change those credentials separately.

<a id="binary"></a>
## Binary release package

The Server is available for Linux amd64 and arm64. Choose the matching architecture; Go, Node.js, and Docker are not required:

```bash
# amd64 for x86_64; arm64 for aarch64
ARCH=amd64
curl -fLO "https://github.com/LeoKon3/MizuPanel/releases/download/v0.1.34/mizupanel-linux-${ARCH}.tar.gz"
tar -xzf "mizupanel-linux-${ARCH}.tar.gz"
cd "mizupanel-linux-${ARCH}"
cp server.example.yaml server.yaml
```

Edit `server.yaml` before starting. For local-only use, set:

```yaml
server:
  listen: "127.0.0.1:8080"
  public_url: ""
  enable_terminal: true
```

Keep the other configuration sections, then run from the extracted directory:

```bash
./mizupanel-server -config server.yaml
```

Open http://127.0.0.1:8080 on the same machine. SQLite defaults to `./data/mizupanel.db`; relative data paths resolve against the working directory. For systemd, set a stable `WorkingDirectory` and adapt the included `systemd/mizupanel-server.service` to your installation path and service user.

Keep the entire package: it contains the Server, web assets, downloadable Agents, and installer scripts. The Server uses CGO and the system C library. If it reports a missing `GLIBC_*` version, use the container image or build on the target distribution; do not replace the system libc. Linux Agents are statically linked. The Windows download is an Agent, not a Windows Server package.

Inspect the version and library requirements:

```bash
./mizupanel-server --version
ldd ./mizupanel-server
```

<a id="build"></a>
## Build from source

Install Go as specified in `go.mod`, Node.js 22, npm, and a C compiler. Include frontend development dependencies:

```bash
npm ci --prefix web --include=dev
NODE_ENV=production make package-linux-amd64 AMD64_CC=gcc
```

The package is written to `dist/mizupanel-linux-amd64.tar.gz`. On a native arm64 host:

```bash
NODE_ENV=production make package-linux-arm64 ARM64_CC=gcc
```

Cross-compiling the arm64 Server on amd64 requires `aarch64-linux-gnu-gcc` (for example, Debian/Ubuntu's `gcc-aarch64-linux-gnu` package). Do not run both packaging targets in parallel; they share staging directories.

Build a local container image:

```bash
docker build -t mizupanel:local .
```

Use `mizupanel:local` in the Docker command, or set `MIZUPANEL_IMAGE=mizupanel:local` in the Compose `.env`.

<a id="remote-access"></a>
## Remote access and authentication

Compose binds to `127.0.0.1` by default for local use or a reverse proxy on the same host. Remote browsers and Agents need a reachable Server address. A remote host's `127.0.0.1` refers to that host, not to the panel.

1. Enable administrator authentication.
2. Use an HTTPS reverse proxy that supports WebSocket, or restrict access to a trusted network.
3. If publishing directly on host interfaces, set `MIZUPANEL_BIND_ADDR=0.0.0.0` and configure the firewall. For a binary deployment, change `server.listen`.
4. Set `server.public_url` when generated Agent commands need a specific reachable URL. Otherwise it is inferred from the request address.

**Compose's `.env` is not automatically passed into a container.** Authentication settings need an explicit override. Create `compose.auth.yml`:

```yaml
services:
  mizupanel:
    environment:
      MIZUPANEL_AUTH_ENABLED: "true"
      MIZUPANEL_ADMIN_USERNAME: ${MIZUPANEL_ADMIN_USERNAME:-admin}
      MIZUPANEL_ADMIN_PASSWORD: ${MIZUPANEL_ADMIN_PASSWORD:?set MIZUPANEL_ADMIN_PASSWORD}
```

Add a strong `MIZUPANEL_ADMIN_PASSWORD` to the protected `.env`. Include the override for startup and subsequent upgrades:

```bash
docker compose -f docker-compose.yml -f compose.auth.yml up -d
# MySQL: use -f docker-compose.mysql.yml -f compose.auth.yml
```

For `docker run`, put the same three variables in a mode-`0600` `auth.env` and add `--env-file ./auth.env` when creating the container. Binary deployments can configure `security.admin` or receive the variables from their service manager. Keep passwords out of command history, images, Git, and public logs.

For a custom Server file, start with `docker/server.sqlite.yaml` or `docker/server.mysql.yaml`, retaining `/app/data` paths and the appropriate storage driver. Mount it read-only and set the container command:

```yaml
services:
  mizupanel:
    command: ["/app/mizupanel-server", "--config", "/app/custom.yaml"]
    volumes:
      - ./server.yaml:/app/custom.yaml:ro
```

Once networking and authentication are configured, generate Agent installation commands from **Hosts → Add Host**.

<a id="backup"></a>
## Backup, upgrade, and rollback

Pin production versions instead of relying on `latest`. Stop writes and take a backup before upgrading. Keep the AI credential key, `ai.key`, together with its database.

SQLite Compose, with the default `./data` directory:

```bash
docker compose stop mizupanel
tar -czf "mizupanel-data-$(date +%Y%m%d-%H%M%S).tar.gz" data
docker compose start mizupanel
```

MySQL:

```bash
docker compose -f docker-compose.mysql.yml stop mizupanel
docker compose -f docker-compose.mysql.yml exec -T mysql \
  sh -c 'MYSQL_PWD="$MYSQL_PASSWORD" exec mysqldump --single-transaction --no-tablespaces -u"$MYSQL_USER" "$MYSQL_DATABASE"' \
  > "mizupanel-db-$(date +%Y%m%d-%H%M%S).sql"
tar -czf "mizupanel-key-$(date +%Y%m%d-%H%M%S).tar.gz" data
docker compose -f docker-compose.mysql.yml start mizupanel
```

Include your authentication override in these commands when applicable, and replace `data` with the actual directory if customized.

Set `MIZUPANEL_IMAGE` in `.env` to the new published version, then pull and recreate:

```bash
docker compose pull mizupanel
docker compose up -d mizupanel
curl -fsS "http://127.0.0.1:${MIZUPANEL_PORT:-8080}/api/system/about"
```

For MySQL, include `-f docker-compose.mysql.yml`. With authentication enabled, verify the version in the signed-in panel or container logs. Rollback requires the old image and a matching pre-upgrade backup; do not assume old code can read a database migrated by a newer version.

`docker compose down` retains named volumes and host data. **`down -v` deletes the MySQL volume**; use it only when intentionally erasing a test environment after confirming backups. Do not use a global `docker system prune --volumes` on an existing deployment.

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

The Dashboard is unauthenticated by default. Enable authentication before remote exposure and restrict network access; see [remote access](#remote-access) for explicitly passing variables through Compose:

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

The recommended path is to open **Hosts → Add Host** and copy the generated Linux or Windows command. The Server generates a short-lived, bootstrap-only `install_token` for each command; its current TTL is 30 minutes.

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
