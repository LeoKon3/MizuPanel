#!/usr/bin/env bash
set -euo pipefail

DEST_ROOT=""
BINARY=""
BINARY_URL=""
BINARY_BASE_URL=""
SERVER_URL=""
TOKEN=""
NODE_ID=""
NAME=""
INTERVAL="5s"
ENABLE_DOCKER="false"
ENABLE_TERMINAL="false"
MODE="normal"

usage() {
  printf 'Usage: %s (--binary PATH | --binary-url URL | --binary-base-url URL) --server-url URL --token TOKEN --node-id ID --name NAME [--interval 5s] [--mode normal|ops] [--enable-docker] [--enable-terminal]\n' "$0"
}

valid_uuid() {
  printf '%s' "$1" | grep -Eiq '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
}

valid_legacy_identity() {
  printf '%s' "$1" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9._:-]{0,190}$'
}

generate_uuid() {
  if command -v uuidgen >/dev/null 2>&1; then
    uuidgen | tr '[:upper:]' '[:lower:]'
    return
  fi
  if [ -r /proc/sys/kernel/random/uuid ]; then
    tr '[:upper:]' '[:lower:]' < /proc/sys/kernel/random/uuid
    return
  fi
  printf 'Unable to generate Agent UUID: uuidgen and /proc/sys/kernel/random/uuid are unavailable.\n' >&2
  exit 1
}

quote_yaml() {
  local value=${1//\\/\\\\}
  value=${value//\"/\\\"}
  printf '"%s"' "$value"
}

docker_group() {
  if [ -S /var/run/docker.sock ]; then
    stat -c '%G' /var/run/docker.sock
    return
  fi
  if getent group docker >/dev/null 2>&1; then
    printf 'docker\n'
    return
  fi
  return 1
}

safe_docker_group() {
  case "$1" in
    ""|root|wheel|sudo|adm)
      return 1
      ;;
    *)
      return 0
      ;;
  esac
}

binary_target() {
  local os arch
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  arch=$(uname -m)
  case "$arch" in
    x86_64|amd64)
      arch="amd64"
      ;;
    aarch64|arm64)
      arch="arm64"
      ;;
    *)
      printf 'Unsupported architecture: %s\n' "$arch" >&2
      exit 1
      ;;
  esac
  case "$os" in
    linux)
      ;;
    *)
      printf 'Unsupported OS: %s\n' "$os" >&2
      exit 1
      ;;
  esac
  printf 'mizupanel-agent-%s-%s' "$os" "$arch"
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --dest-root)
      DEST_ROOT="$2"
      shift 2
      ;;
    --binary)
      BINARY="$2"
      shift 2
      ;;
    --binary-url)
      BINARY_URL="$2"
      shift 2
      ;;
    --binary-base-url)
      BINARY_BASE_URL="$2"
      shift 2
      ;;
    --server-url)
      SERVER_URL="$2"
      shift 2
      ;;
    --token)
      TOKEN="$2"
      shift 2
      ;;
    --node-id)
      NODE_ID="$2"
      shift 2
      ;;
    --name)
      NAME="$2"
      shift 2
      ;;
    --interval)
      INTERVAL="$2"
      shift 2
      ;;
    --mode)
      MODE="$2"
      shift 2
      ;;
    --enable-docker)
      ENABLE_DOCKER="true"
      shift
      ;;
    --enable-terminal)
      ENABLE_TERMINAL="true"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf 'Unknown argument: %s\n' "$1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [ -z "$SERVER_URL" ] || [ -z "$TOKEN" ] || [ -z "$NODE_ID" ] || [ -z "$NAME" ]; then
  usage >&2
  exit 1
fi
case "$MODE" in
  normal|ops)
    ;;
  *)
    printf 'Unsupported mode: %s\n' "$MODE" >&2
    usage >&2
    exit 1
    ;;
esac
if [ "$MODE" = "ops" ]; then
  printf '警告：运维模式会以 root 用户运行 Agent，可执行终端、文件编辑和重启等高权限操作。\n' >&2
fi
if [ -z "$BINARY" ] && [ -z "$BINARY_URL" ] && [ -z "$BINARY_BASE_URL" ]; then
  usage >&2
  exit 1
fi
BINARY_SOURCE_COUNT=0
[ -n "$BINARY" ] && BINARY_SOURCE_COUNT=$((BINARY_SOURCE_COUNT + 1))
[ -n "$BINARY_URL" ] && BINARY_SOURCE_COUNT=$((BINARY_SOURCE_COUNT + 1))
[ -n "$BINARY_BASE_URL" ] && BINARY_SOURCE_COUNT=$((BINARY_SOURCE_COUNT + 1))
if [ "$BINARY_SOURCE_COUNT" -ne 1 ]; then
  printf 'Pass only one of --binary, --binary-url, or --binary-base-url.\n' >&2
  exit 1
fi
if [ -n "$BINARY" ] && [ ! -f "$BINARY" ]; then
  printf 'Agent binary not found: %s\n' "$BINARY" >&2
  exit 1
fi
if [ -n "$BINARY_BASE_URL" ]; then
  BINARY_URL="${BINARY_BASE_URL%/}/$(binary_target)"
fi
if [ -z "$DEST_ROOT" ] && [ "$(id -u)" -ne 0 ]; then
  printf 'Please run this installer as root.\n' >&2
  exit 1
fi

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
SERVICE_TEMPLATE="$SCRIPT_DIR/../systemd/mizupanel-agent.service"
INSTALL_DIR="$DEST_ROOT/usr/local/mizupanel"
BIN_DIR="$INSTALL_DIR/bin"
ETC_DIR="$INSTALL_DIR/etc"
VAR_DIR="$INSTALL_DIR/var"
BACKUP_DIR="$VAR_DIR/backups"
COMPOSE_DIR="$DEST_ROOT/var/lib/mizupanel/compose"
IDENTITY_PATH="$ETC_DIR/agent-id"
CONFIG_PATH="$ETC_DIR/agent.yaml"
SYSTEMD_DIR="$DEST_ROOT/etc/systemd/system"

if [ -z "$DEST_ROOT" ] && ! id -u mizupanel-agent >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin mizupanel-agent
fi

install -d -m 0755 "$INSTALL_DIR" "$BIN_DIR" "$ETC_DIR" "$VAR_DIR" "$BACKUP_DIR"
install -d -m 0750 "$COMPOSE_DIR"
if [ -z "$DEST_ROOT" ]; then
  chown root:root "$INSTALL_DIR"
  chmod 0755 "$INSTALL_DIR"
fi
install -d -m 0755 "$SYSTEMD_DIR"

if [ -f "$IDENTITY_PATH" ]; then
  AGENT_ID=$(tr -d '[:space:]' < "$IDENTITY_PATH")
  if ! valid_uuid "$AGENT_ID" && ! valid_legacy_identity "$AGENT_ID"; then
    printf 'Invalid Agent identity in %s. Fix or explicitly remove it before reinstalling.\n' "$IDENTITY_PATH" >&2
    exit 1
  fi
elif [ -f "$INSTALL_DIR/agent.yaml" ]; then
  LEGACY_NODE_ID=$(awk '/^[[:space:]]*id:[[:space:]]*/ { sub(/^[[:space:]]*id:[[:space:]]*/, ""); gsub(/^"|"$/, ""); print; exit }' "$INSTALL_DIR/agent.yaml")
  if valid_uuid "$LEGACY_NODE_ID"; then
    AGENT_ID=$(printf '%s' "$LEGACY_NODE_ID" | tr '[:upper:]' '[:lower:]')
  elif valid_legacy_identity "$LEGACY_NODE_ID"; then
    AGENT_ID="$LEGACY_NODE_ID"
  else
    AGENT_ID=$(generate_uuid)
  fi
else
  AGENT_ID=$(generate_uuid)
fi
IDENTITY_TMP=$(mktemp "$ETC_DIR/agent-id.XXXXXX")
printf '%s\n' "$AGENT_ID" > "$IDENTITY_TMP"
chmod 0600 "$IDENTITY_TMP"
install -m 0600 "$IDENTITY_TMP" "$IDENTITY_PATH"
rm -f "$IDENTITY_TMP"

if [ -n "$BINARY_URL" ]; then
  BINARY_TMP=$(mktemp "$BIN_DIR/mizupanel-agent.XXXXXX")
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$BINARY_URL" -o "$BINARY_TMP"
  else
    wget -qO "$BINARY_TMP" "$BINARY_URL"
  fi
  if [ -f "$BIN_DIR/mizupanel-agent" ]; then
    install -m 0755 "$BIN_DIR/mizupanel-agent" "$BACKUP_DIR/mizupanel-agent.previous"
  elif [ -f "$INSTALL_DIR/mizupanel-agent" ]; then
    install -m 0755 "$INSTALL_DIR/mizupanel-agent" "$BACKUP_DIR/mizupanel-agent.previous"
  fi
  install -m 0755 "$BINARY_TMP" "$BIN_DIR/mizupanel-agent"
  rm -f "$BINARY_TMP"
else
  if [ -f "$BIN_DIR/mizupanel-agent" ]; then
    install -m 0755 "$BIN_DIR/mizupanel-agent" "$BACKUP_DIR/mizupanel-agent.previous"
  elif [ -f "$INSTALL_DIR/mizupanel-agent" ]; then
    install -m 0755 "$INSTALL_DIR/mizupanel-agent" "$BACKUP_DIR/mizupanel-agent.previous"
  fi
  install -m 0755 "$BINARY" "$BIN_DIR/mizupanel-agent"
fi

CONFIG_TMP=$(mktemp "$ETC_DIR/agent.yaml.XXXXXX")
chmod 0600 "$CONFIG_TMP"
{
  printf 'server:\n'
  printf '  url: '
  quote_yaml "$SERVER_URL"
  printf '\n'
  printf '  token: '
  quote_yaml "$TOKEN"
  printf '\n'
  printf 'node:\n'
  printf '  id: '
  quote_yaml "$AGENT_ID"
  printf '\n'
  printf '  name: '
  quote_yaml "$NAME"
  printf '\n'
  printf 'runtime:\n'
  printf '  interval: '
  quote_yaml "$INTERVAL"
  printf '\n'
  printf '  mode: '
  quote_yaml "$MODE"
  printf '\n'
  printf 'features:\n'
  printf '  docker: %s\n' "$ENABLE_DOCKER"
  printf '  terminal: %s\n' "$ENABLE_TERMINAL"
} > "$CONFIG_TMP"
install -m 0600 "$CONFIG_TMP" "$CONFIG_PATH"
rm -f "$CONFIG_TMP"

if [ -z "$DEST_ROOT" ]; then
  chown root:root "$INSTALL_DIR" "$BIN_DIR" "$ETC_DIR" "$VAR_DIR" "$BACKUP_DIR" "$COMPOSE_DIR" "$BIN_DIR/mizupanel-agent" "$IDENTITY_PATH"
  if [ "$MODE" = "ops" ]; then
    chown root:root "$CONFIG_PATH"
  else
    chown mizupanel-agent:mizupanel-agent "$CONFIG_PATH" "$IDENTITY_PATH"
    chown mizupanel-agent:mizupanel-agent "$COMPOSE_DIR"
    chmod 0750 "$COMPOSE_DIR"
  fi
  if [ "$MODE" != "ops" ] && [ "$ENABLE_DOCKER" = "true" ]; then
    DOCKER_GROUP=$(docker_group) || {
      printf 'Docker monitoring was requested, but Docker socket/group was not found. Install Docker first or rerun without --enable-docker.\n' >&2
      exit 1
    }
    if ! safe_docker_group "$DOCKER_GROUP"; then
      printf 'Docker monitoring was requested, but Docker socket group "%s" is too privileged. Configure Docker to use a dedicated docker group first.\n' "$DOCKER_GROUP" >&2
      exit 1
    fi
    usermod -aG "$DOCKER_GROUP" mizupanel-agent
  fi
fi

if [ -f "$SERVICE_TEMPLATE" ]; then
  install -m 0644 "$SERVICE_TEMPLATE" "$SYSTEMD_DIR/mizupanel-agent.service"
else
  cat > "$SYSTEMD_DIR/mizupanel-agent.service" <<'SERVICE'
[Unit]
Description=MizuPanel Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/mizupanel/bin/mizupanel-agent -config /usr/local/mizupanel/etc/agent.yaml
Restart=always
RestartSec=5s
User=mizupanel-agent
Group=mizupanel-agent
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/usr/local/mizupanel/etc/agent.yaml /var/lib/mizupanel/compose

[Install]
WantedBy=multi-user.target
SERVICE
fi

sed -i \
  -e 's#/usr/local/mizupanel/mizupanel-agent#/usr/local/mizupanel/bin/mizupanel-agent#g' \
  -e 's#/usr/local/mizupanel/agent.yaml#/usr/local/mizupanel/etc/agent.yaml#g' \
  "$SYSTEMD_DIR/mizupanel-agent.service"

if [ "$MODE" = "ops" ]; then
  sed -i \
    -e 's/^User=.*/User=root/' \
    -e 's/^Group=.*/Group=root/' \
    -e '/^NoNewPrivileges=/d' \
    -e '/^ProtectSystem=/d' \
    -e '/^ProtectHome=/d' \
    -e '/^ReadWritePaths=/d' \
    "$SYSTEMD_DIR/mizupanel-agent.service"
fi

if [ -z "$DEST_ROOT" ]; then
  systemctl daemon-reload
  systemctl enable mizupanel-agent
  if ! systemctl restart mizupanel-agent || ! systemctl is-active --quiet mizupanel-agent; then
    if [ -f "$BACKUP_DIR/mizupanel-agent.previous" ]; then
      install -m 0755 "$BACKUP_DIR/mizupanel-agent.previous" "$BIN_DIR/mizupanel-agent"
      systemctl restart mizupanel-agent || true
    fi
    printf 'MizuPanel agent upgrade failed; the previous binary was restored when available.\n' >&2
    exit 1
  fi
fi

printf 'MizuPanel agent installed or upgraded. Agent ID: %s Config: %s\n' "$AGENT_ID" "$CONFIG_PATH"
