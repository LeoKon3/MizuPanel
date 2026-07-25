# Changelog

All notable changes to MizuPanel will be documented in this file.

## v0.1.6 - 2026-07-25

### Added

- Added configuration-independent `version`, `--version`, and `-v` commands to both Server and Agent binaries.

### Changed

- Bumped the Server, Dashboard package metadata, README badges, and Server-bundled Agent release version to 0.1.6.

## v0.1.5 - 2026-07-25

### Added

- Added Agent-managed Compose application creation and updates with YAML validation, risk preview, short-lived content-bound confirmation tokens, optional image pulls, and fixed `up -d` execution.
- Added managed Compose rollback and recoverable archive workflows, bounded YAML-only revisions, managed/external source badges, and capability-gated Dashboard controls.
- Added the `docker_compose_deployment` Agent capability and structured deployment messages/API while keeping older Agents on the existing Compose management flow.

### Changed

- Changed Linux Agent installation and systemd sandboxing to provision the private managed Compose root at `/var/lib/mizupanel/compose` for both normal and ops modes.
- Bumped the Server, Dashboard package metadata, README badges, and Server-bundled Agent release version to 0.1.5.
- Standardized patch-version bumps for every user-facing bug-fix or feature batch, with a synchronization test and release-tag guard covering all current version surfaces.

### Security

- Managed Compose projects use Agent-generated UUID directories, fixed Docker CLI arguments, atomic private files, `0600` `.env` permissions, path/symlink checks, and YAML guardrails that reject builds, profiles, Compose CLI control variables, and external configuration includes.
- Managed Compose YAML, `.env` values, local paths, revisions, and raw Docker diagnostics are never persisted by the Server or returned in deployment responses.

## v0.1.4 - 2026-07-25

### Added

- Added optional admin authentication (Auth v1) for Dashboard access protection.
- Added `security.admin.*` configuration fields for enabling authentication, setting username/password, and configuring session TTL.
- Added environment variable overrides: `MIZUPANEL_AUTH_ENABLED`, `MIZUPANEL_ADMIN_USERNAME`, `MIZUPANEL_ADMIN_PASSWORD`, `MIZUPANEL_SESSION_TTL`.
- Added `/api/auth/session`, `/api/auth/login`, and `/api/auth/logout` endpoints for authentication flow.
- Added in-memory session management with HttpOnly session cookies (SameSite=Lax).
- Added Dashboard login page with username/password form when authentication is enabled and user is not logged in.
- Added logout button in Dashboard header when user is authenticated.
- Added authentication middleware protecting sensitive Dashboard APIs: `/api/settings`, `/api/nodes`, `/api/nodes/*`, `/api/install/*`.
- Added Alert System v1 with metric-based alert rules and notification channels.
- Added `alert_rules` and `alert_history` database tables with SQLite and MySQL migrations.
- Added alert rules CRUD API: create, list, update, delete, and toggle enable/disable.
- Added alert history query API by node ID with configurable limit.
- Added alert engine with 30-second polling (configurable via `alerting.check_interval`).
- Added support for CPU, memory, disk, swap usage and system load metrics monitoring.
- Added comparison operators: `>`, `>=`, `<`, `<=`, `=` for threshold evaluation.
- Added duration-based alert conditions (alert only triggers after condition persists for specified seconds).
- Added node scope filtering: "all nodes" or "specific node IDs".
- Added Webhook notification channel with custom headers support.
- Added DingTalk robot notification channel with HMAC-SHA256 signature support.
- Added alert state tracking in memory to prevent duplicate notifications.
- Added automatic alert resolution tracking (updates `resolved_at` when condition no longer met).
- Added Feishu (飞书) notification channel with interactive card format and HMAC-SHA256 signature support.
- Added notification channel buttons for DingTalk and Feishu in the alert rule form.
- Added concurrent alert delivery with bounded retries for transient Webhook, DingTalk, and Feishu failures.
- Added persisted trigger and recovery notification attempt/result fields to alert history.
- Added recovery notifications for real metric recovery while keeping manual resolution, rule disabling, and rule deletion silent.
- Added Dashboard "告警规则" page with rule list, create/edit/delete forms, and enable/disable controls.
- Added alert icon to Dashboard sidebar navigation.
- Added Kubernetes cluster management across Agent kubectl handling, protocol messages, Server storage/service/API, and Dashboard pages.
- Added Kubernetes cluster connection dialog, cluster list page, cluster detail page, and Pod logs modal.
- Added Kubernetes resource views for Namespaces, Nodes, Pods, Deployments, StatefulSets, DaemonSets, Services, and Ingresses.
- Added Kubernetes diagnostics drawer with overview, Events, Describe, YAML, logs, related Pod status, and YAML dry-run/apply editing.
- Added Kubernetes current Pod/container resource observation from metrics-server and compact node resource summaries with capacity, allocatable, usage, and Pod count.
- Added workload Pod expansion views for Deployments, StatefulSets, and DaemonSets with related Pod resource/status breakdown.
- Added cluster-level Kubernetes manifest apply flow with dry-run support through REST API, Server Agent Hub, Agent kubectl client, and frontend client.
- Added Kubernetes create resource modal for Deployment, Pod, StatefulSet, DaemonSet, Job, CronJob, Service, Ingress, ConfigMap, Secret, PVC, and custom YAML.
- Added advanced Kubernetes resource creation options for namespaces, multi-container workloads, init containers, commands/args, environment variables, ConfigMap/Secret refs, probes, resources, volumes, scheduling, security context, update strategy, Service ports, Ingress routing, PVC storage, Job policy, CronJob policy, and StatefulSet volume claim templates.
- Added manual alert history resolution plus resolved-alert single and bulk deletion APIs and Dashboard controls.
- Added `/api/system/about` and `VERSION`-backed system metadata for Dashboard settings.
- Added GitHub repository link and version display to the System Settings page.
- Added refreshed README screenshot gallery and separate detailed configuration/screenshot documentation pages in Chinese and English.
- Added GitHub Actions release workflow to build and upload Linux amd64/arm64 release packages for `v*` tags and manual tag dispatch.
- Added persistent Agent identity under `/usr/local/mizupanel/etc/agent-id`, generated once and reused across idempotent reinstall and upgrade runs.
- Added Agent protocol version, capability reporting, connection diagnostics, and identity-conflict detection.
- Added secure one-click Agent upgrades to the current Server version with SHA-256 verification, architecture checks, delayed rollback, reconnect confirmation, and asynchronous failure reporting.
- Added node groups and preset-color tags with relational SQLite/MySQL storage, enriched node APIs, and transactional batch metadata updates.
- Added grouped host browsing, group/tag filters, a desktop batch-management table, and latest-version batch Agent upgrades with bounded frontend concurrency.
- Added a single-host “分组与标签” editor in the host detail header, including offline editing, inline tag creation, and local node refresh after save.
- Added structured Docker Compose project discovery and lifecycle operations through the Agent, including project/service status, image pull, start/recreate, restart, stop, and guarded down actions.
- Added a desktop host-detail Docker view switch between individual containers and Compose projects, with service tables, capability/empty states, loading feedback, and custom destructive-action confirmation.
- Added Agent-side persistence for discovered Compose projects under `/usr/local/mizupanel/var/compose-projects.json`, allowing projects to be listed and started again after `docker compose down`.
- Added guarded Compose project logs (`--no-color --tail 200`) and configuration validation (`config --quiet`), plus service-level log and terminal entry points in the host detail view.
- Added Compose service lifecycle actions for image pull, start/recreate, restart, and stop. These are scoped to an Agent-discovered service and presented in the service-row more menu.
- Added Systemd service management in the host detail page: list loaded `.service` units, filter/search their current state, inspect recent logs, and run start, stop, or restart actions.
- Added the `systemd_services` Agent capability with structured Systemd list/action protocol messages and node APIs, so unsupported or older Agents remain safely compatible.
- Added Docker resource management for images, volumes, and networks, including Docker disk-usage summaries, search and usage filters, image pulls, and guarded deletion of unused resources.
- Added the `docker_resources` Agent capability with structured list/action messages and node APIs; image, volume, and network deletion is revalidated by the Agent without force or prune semantics.

### Changed

- Agent WebSocket connections (`/api/agent/ws`) remain public and use existing node token authentication.
- Default authentication is disabled (`security.admin.enabled: false`) to preserve existing deployment behavior.
- Session storage is in-memory; Server restart requires re-login.
- Alert engine runs in background goroutine, checks all enabled rules every 30 seconds by default.
- Alert state is memory-only; service restart loses tracking state but preserves history records.
- Changed the add-host flow to focus on generated curl/PowerShell install commands for the single-host dialog.
- Changed the Dashboard navigation and branding with the soft-tech logo, simplified main nav labels, and hidden global history/log entries while preserving their routes.
- Changed Kubernetes filters to use namespace dropdown selection and resource search patterns that keep namespace filtering separate from name/status searching.
- Changed Kubernetes row actions to use softer body-portaled "more" menus so table actions remain usable near scroll boundaries.
- Changed Kubernetes table density so deeper CPU/memory/container details live in drawers or expanded rows instead of crowding the main tables.
- Changed Kubernetes YAML display/editing to remove noisy managed-fields output from normal viewing and require dry-run before saving edits.
- Changed release packaging to include the `VERSION` file and documented direct GitHub Release package downloads.
- Reworked `README.md` and `README.en.md` as visual product introductions with release-package deployment first, current screenshots, and detailed setup moved to docs pages.
- Updated screenshot assets to match the current Dashboard, host, Kubernetes, create-resource, and alert interfaces.
- Changed Linux Agent installation to the `/usr/local/mizupanel/{bin,etc,var}` layout and made repeat installation preserve the machine identity while refreshing binaries and configuration.
- Changed Agent upgrade policy to support only the latest Server-bundled Agent version; legacy Agents receive a final idempotent install command instead of a selectable target version.
- Changed alert channel delivery to treat a notification as successful only when every configured channel succeeds, with sanitized per-channel failure summaries.
- Changed the host workspace to separate single-host browsing from multi-host batch management while sharing search, status, group, and tag filters.
- Changed the supported Dashboard surface to Windows desktop browsers: one persistent sidebar and one operational batch table with horizontal overflow, without phone-specific navigation or duplicate list rendering.
- Changed Docker operations to advertise Compose capability through the Agent hello handshake while keeping older Agents compatible and lazily loading Compose data only when the view is opened.
- Changed Compose project discovery to merge valid local cache entries with live Docker output, while ignoring missing configuration files and non-blocking cache I/O failures.
- Changed Compose validation errors to hide sensitive values before returning them to the Server and Dashboard.
- Changed Compose service actions to require an explicit Agent capability handshake; older Agents keep their existing project operations and never receive a service-scoped request.
- Bumped the Server, Dashboard package metadata, and Server-bundled Agent release version to 0.1.4; older Agents continue to detect and install only the current bundled build.
- Changed host-detail service operations to keep `mizupanel-agent.service` in the dedicated Agent management page rather than exposing it in the general Systemd service table.
- Changed the host-detail Docker workspace to merge containers, Compose projects, and Docker resources into three desktop views with a shared high-density visual structure.

### Fixed

- Fixed the Docker container toolbar so view tabs, status filters, search, refresh, and create controls no longer collapse into vertically wrapped text in constrained desktop detail panels.
- Fixed Kubernetes cluster detail navigation, empty/null cluster response handling, and connection timeout behavior.
- Fixed Overview "添加服务器" quick action so it opens the existing add-host dialog instead of doing nothing/use a browser alert.
- Fixed long Kubernetes status badges overflowing table and drawer layouts.
- Fixed alert history rule names so renamed rules are reflected when history is listed.
- Fixed disabled alert rules so currently active alerts are resolved instead of remaining active indefinitely.
- Fixed resolved alert cleanup so active alerts cannot be deleted before being resolved.
- Fixed Agent identity instability across reinstall and upgrade by preserving the generated machine UUID independently from mutable display names and hostnames.
- Fixed duplicate Agent connections with the same identity so the Server can distinguish replacement from suspected identity conflicts.
- Fixed transient alert delivery loss by retrying network errors, HTTP 408/425/429, and 5xx responses while avoiding deterministic 4xx retries.
- Fixed alert history so trigger and recovery delivery status remains distinguishable from unknown legacy rows.
- Fixed the Compose service table so logs, terminal, and lifecycle operations are consolidated into one more menu instead of crowding the action column.
- Fixed the Systemd service table action column to remain visible in the desktop detail panel, with its more menu positioned inside the viewport.

### Security

- Password comparison uses constant-time `crypto/subtle.ConstantTimeCompare` to prevent timing attacks.
- Passwords are stored in plaintext in configuration files (self-use scope, not hashed).
- Empty passwords are rejected when authentication is enabled.
- Session TTL defaults to 24 hours; expired sessions are automatically pruned.
- Alert rules API endpoints are protected by authentication middleware when auth is enabled.
- Agent upgrades accept only fixed Server-origin artifacts, cap downloads at 256 MiB, verify SHA-256 and executable ELF architecture, and arm rollback before replacing the running binary.
- Connection and alert errors exposed to logs, APIs, and history omit tokens, Webhook URLs, secrets, and custom request headers.
- Compose service actions use a fixed operation allowlist and require the requested service to be present in the Agent's current project discovery result.
- Systemd operations accept only validated `.service` names and the fixed `start`, `stop`, `restart`, and bounded-log actions; the Agent validates that a service is currently loaded, serializes concurrent actions for that service, and redacts common sensitive values from logs.

## v0.0.4 - 2026-06-10

### Added

- Added a node-level Agent 管理 tab for checking Agent status, recent Agent logs, and sending Agent restart requests.
- Added Agent management request/response messages across protocol, Agent WebSocket handling, Server agent hub forwarding, and node API routes.
- Added local Agent management handlers for runtime, Docker availability, restart acceptance, and bounded recent log reads.

### Fixed

- Fixed Agent 管理 UI state handling so stale status/log data is cleared when switching nodes or when a node becomes offline.
- Fixed API error handling so JSON `error` messages from Agent management endpoints are shown instead of generic HTTP failures.
- Fixed Docker capability reporting to reflect actual collector availability and errors instead of only the configured Docker monitoring flag.

## v0.0.3 - 2026-06-03

### Added

- Added real Agent uptime, disk read speed, and disk write speed collection across Agent, protocol, Server persistence, API responses, and Dashboard charts.
- Added node detail display for derived boot time and formatted runtime based on reported uptime.
- Added disk I/O chart summaries for read/write throughput.

### Changed

- Refined the Dashboard layout with smoother sidebar collapse/expand animation and a cleaner content header.
- Moved the sidebar collapse control to the outside edge of the sidebar so the collapsed sidebar keeps the logo at the top.
- Simplified Agent installation for self-use root deployments: Linux SSH and manual install now default to root ops mode with terminal access and Docker monitoring enabled.
- Removed Agent install option controls for terminal, Docker monitoring, and run mode from both SSH automatic install and manual install flows.
- Updated manual install copy to use platform-neutral wording where Linux and Windows commands share the same flow.

### Fixed

- Fixed disk I/O display for legacy or missing metrics so the Dashboard shows placeholders instead of `NaN undefined/s`.
- Fixed boot time and runtime display when metric history rows are missing new uptime fields.
- Fixed manual install command generation so Linux install strategy options are owned by the Server instead of frontend query parameters.

## v0.0.2 - 2026-06-01

### Added

- Added the first project changelog.
- Added Docker deployment support with a root `Dockerfile`, default SQLite `docker-compose.yml`, optional MySQL `docker-compose.mysql.yml`, and Docker-specific server configs under `docker/`.
- Added optional MySQL storage support alongside the default SQLite storage mode.
- Added database dialect handling for SQLite/MySQL migrations and upsert SQL.
- Added system settings for metrics retention, with runtime updates persisted in the database.
- Added metrics history views with selectable ranges for CPU, memory, disk, network, and load data.
- Added in-panel node record removal confirmation dialog instead of the browser-native confirm dialog.
- Added README architecture SVG at `assets/mizupanel-architecture.svg`.
- Added README screenshot gallery with Dashboard, metrics history, system settings, add-host, and Web terminal screenshots under `assets/screenshots/`.
- Added Linux root-only SSH Agent install jobs with one-time password/private-key credentials and Server-Sent Events progress.
- Added Linux root-only SSH Agent uninstall jobs from node details, including optional panel record removal and progress events.
- Added SSH automatic install controls for terminal access, Docker monitoring, and Agent run mode in the add-host dialog.
- Added Chinese default README with English switch link and a separate `README.en.md`.

### Changed

- Kept SQLite as the default storage backend for simple self-hosted deployments.
- Updated server configuration to support structured storage settings for SQLite and MySQL.
- Updated metric retention validation to use supported values: `6h`, `24h`, `3d`, and `7d`.
- Updated Docker Compose defaults to bind the panel to `127.0.0.1` unless `MIZUPANEL_BIND_ADDR=0.0.0.0` is explicitly set.
- Reorganized README content to prioritize core features, Docker quick start, architecture, release package deployment, and Agent setup.
- Condensed Agent install commands into collapsible README sections.
- Changed Linux manual Agent install/uninstall commands to root-only execution instead of `sudo` wrappers.
- Changed SSH install fallback identity handling to use the remote hostname when no node ID is provided.
- Reworked token documentation into a table and registration flow.

### Fixed

- Fixed Vite proxy behavior so same-origin protected API operations such as deleting node records work through the dev server.
- Fixed Vite dev proxy WebSocket forwarding so browser terminal connections under `/api` work through the frontend dev server.
- Fixed metrics API behavior for missing nodes by returning `404` instead of empty history data.
- Fixed history range selection so ranges beyond the configured retention are disabled and rejected.
- Fixed MySQL migration compatibility for existing schemas by using MySQL-compatible column definitions.
- Fixed SSH automatic install option changes so they no longer create unused manual install tokens while the SSH tab is active.
- Fixed the add-host dialog so closing it clears one-time SSH credential fields and install progress state.

### Security

- Avoided exposing database passwords through browser APIs or logs.
- Changed MySQL Docker credentials to required environment variables instead of hardcoded deployment passwords.
- Preserved Agent's active-connection model so target hosts do not expose Agent ports.
- Changed SSH install/uninstall remote scripts to use unique `mktemp` paths instead of predictable `/tmp` script names.
