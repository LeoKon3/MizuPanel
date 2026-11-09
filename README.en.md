<p align="center">
  <img src="web/public/logo.svg" alt="MizuPanel" width="64" height="64" />
</p>

<h1 align="center">MizuPanel</h1>

<p align="center">A self-hosted operations panel for personal servers and small fleets.</p>

<p align="center">
  <a href="README.md">中文</a> · English ·
  <a href="https://github.com/LeoKon3/MizuPanel/releases">Downloads</a> ·
  <a href="docs/configuration.en.md">Deployment guide</a>
</p>

<p align="center">
  <a href="https://github.com/LeoKon3/MizuPanel/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/LeoKon3/MizuPanel/actions/workflows/ci.yml/badge.svg" /></a>
  <img alt="Version" src="https://img.shields.io/badge/version-0.1.34-1b6c80" />
</p>

MizuPanel brings host monitoring, container management, service troubleshooting, and routine operations into one workspace. It is designed for personal servers, homelabs, and small multi-host environments. The Server hosts the panel and stores data; Agents connect to it, report metrics, and execute authorized operations. SQLite is the default database, with MySQL available as an alternative.

## Features

- **Hosts**: CPU, memory, disk, network, and historical metrics, plus groups, tags, processes, file management, and a web terminal.
- **Docker and Systemd**: manage containers, Compose applications, and system services; inspect logs and handle deployments and updates.
- **Application services**: associate hosts, workloads, probes, and alerts with a logical service to see its health, resources, and recent events together.
- **Kubernetes**: inspect clusters and workloads, read logs and events, and manage YAML and common resources.
- **Alerts and uptime**: metric alerts, HTTP/HTTPS/TCP checks, TLS expiry warnings, and failure/recovery notifications.
- **Automation and audit**: a script library, multi-host tasks, Cron schedules, execution history, and audit records for sensitive operations.
- **AI assistant**: query resources and investigate issues through an OpenAI Chat Completions-compatible model. Every state-changing operation requires human confirmation.

## Preview

[![MizuPanel overview](assets/screenshots/overview-light.png)](docs/screenshots.en.md)

Light and dark themes are available. [View more screenshots](docs/screenshots.en.md).

## Quick start

**Docker Compose** is the recommended deployment method. Install Docker Engine and the Compose plugin first; Go and Node.js are not required.

```bash
mkdir -p mizupanel && cd mizupanel
curl -fLO https://raw.githubusercontent.com/LeoKon3/MizuPanel/v0.1.34/docker-compose.yml
docker compose up -d
```

Open **http://127.0.0.1:8080** on the Docker host. The default binding is local-only, and persistent data is stored in the current directory's `data/` folder.

> These commands require the corresponding published image. If you are reading an unreleased source version, choose a published version from [Releases](https://github.com/LeoKon3/MizuPanel/releases), or build from source using the deployment guide.

### Add a host

1. Open **Hosts → Add Host** in the panel.
2. Copy the Agent installation command for the target operating system and run it on that host.
3. Return to the panel to check its connection and metrics.

Remote Agents must be able to reach the panel. Before exposing it, configure administrator authentication, a reachable address, and HTTPS; see [remote access and authentication](docs/configuration.en.md#remote-access). Do not expose the default unauthenticated panel directly to the internet.

### Other deployment options

| Method | Use case |
| --- | --- |
| [Binary release package](docs/configuration.en.md#binary) | Run the Linux amd64/arm64 Server without containers |
| [Docker command](docs/configuration.en.md#docker-run) | Manage a single container with `docker run` |
| [Compose with MySQL](docs/configuration.en.md#mysql) | Use MySQL for panel data |
| [Build from source](docs/configuration.en.md#build) | Local development, testing, or custom builds |

## Data and security

- Agents run on the managed hosts. Grant terminal, Docker, and task execution capabilities only to trusted administrators.
- Back up the SQLite data directory or MySQL database together with the AI credential key, `ai.key`. Take a backup before upgrades or rollbacks.
- AI is optional. Before connecting an external model, decide which data may be sent to that service. Human confirmation does not replace host and network access controls.

## Documentation and feedback

- [Configuration and deployment](docs/configuration.en.md): authentication, Agents, MySQL, backups, upgrades, and rollback.
- [Screenshots](docs/screenshots.en.md)
- [Changelog](CHANGELOG.md)
- [Release process](RELEASING.md)
- [Issues and feature requests](https://github.com/LeoKon3/MizuPanel/issues)

Licensed under [MIT](LICENSE). Thanks to the [Linux.do](https://linux.do/) community for its feedback and support.
