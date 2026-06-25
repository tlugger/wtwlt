#!/usr/bin/env bash
#
# wtwlt server installer / updater for the Raspberry Pi.
#
#   curl -fsSL https://raw.githubusercontent.com/tlugger/wtwlt/main/install.sh | sudo bash
#
# Downloads the latest release binary for this machine's architecture (or builds
# from source as a fallback), installs a systemd service, and starts it.
# Re-running upgrades in place. Idempotent.
set -euo pipefail

INSTALL_DIR="${WTWLT_INSTALL_DIR:-/home/pi/wtwlt}"
REPO="tlugger/wtwlt"
SERVICE_NAME="wtwlt"
BINARY="wtwlt-server"
CURR_DIR="$(pwd)"

# ── pretty output ───────────────────────────────────────────────────
spin() {
  local pid=$1 msg=$2
  local frames=("⠋" "⠙" "⠹" "⠸" "⠼" "⠴" "⠦" "⠧" "⠇" "⠏")
  local i=0
  while kill -0 "$pid" 2>/dev/null; do
    printf "\r  %s %s" "${frames[$((i % 10))]}" "$msg"
    i=$((i + 1)); sleep 0.1
  done
  if wait "$pid"; then printf "\r  ✅ %s\n" "$msg"; else printf "\r  ❌ %s\n" "$msg"; return 1; fi
}
step() { echo ""; echo "── $1 ──"; }
ok()   { echo "  ✅ $1"; }
warn() { echo "  ⚠️  $1"; }
fail() { echo "  ❌ $1"; exit 1; }

[ "$(id -u)" -eq 0 ] || fail "Please run as root (pipe into 'sudo bash')."

mkdir -p "$INSTALL_DIR"

# ── banner ──────────────────────────────────────────────────────────
if [ -f "$INSTALL_DIR/$BINARY" ]; then
  echo ""; echo "  ⛅ wtwlt updater"; echo "  ───────────────"; echo "  Updating existing installation"
else
  echo ""; echo "  ⛅ wtwlt installer"; echo "  ─────────────────"; echo "  Home weather station server"
fi
echo ""

stop_service() {
  if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
    systemctl stop "$SERVICE_NAME"; ok "Stopped running service"
  fi
}

# ── configuration (.env) ────────────────────────────────────────────
if [ "$CURR_DIR" != "$INSTALL_DIR" ] && [ -f "$CURR_DIR/.env" ]; then
  step "Loading .env"
  cp "$CURR_DIR/.env" "$INSTALL_DIR/.env"; chmod 600 "$INSTALL_DIR/.env"
  ok "Copied .env from current directory"
elif [ -f "$INSTALL_DIR/.env" ]; then
  ok "Using existing .env"
else
  step "Configuration"
  cat > "$INSTALL_DIR/.env" <<'EOF'
# wtwlt server configuration (defaults assume a local, anonymous broker)
WTWLT_HTTP_ADDR=:8080
WTWLT_MQTT_HOST=localhost
WTWLT_MQTT_PORT=1883
WTWLT_MQTT_USER=
WTWLT_MQTT_PASS=
WTWLT_RETENTION_DAYS=90

# Forecast overlay (keyless). Provider: openmeteo (default) | nws | none.
# Set the station's coordinates to enable it (blank = forecast off). They are
# used only to fetch the forecast and to derive a coarse city label; the exact
# coordinates are never shown in the dashboard.
WTWLT_FORECAST_PROVIDER=openmeteo
WTWLT_LAT=
WTWLT_LON=
EOF
  chmod 600 "$INSTALL_DIR/.env"
  ok "Created $INSTALL_DIR/.env (local broker, port 8080, 90-day retention)"
fi

# ── obtain the binary ───────────────────────────────────────────────
if [ "$CURR_DIR" != "$INSTALL_DIR" ] && [ -f "$CURR_DIR/$BINARY" ]; then
  step "Installing local binary"
  stop_service
  cp "$CURR_DIR/$BINARY" "$INSTALL_DIR/$BINARY"; chmod +x "$INSTALL_DIR/$BINARY"
  ok "Installed from current directory"
else
  step "Detecting system"
  ARCH=$(uname -m)
  case "$ARCH" in
    aarch64|arm64)       GOARCH="arm64" ;;
    armv7l|armv6l|armhf) GOARCH="arm" ;;
    x86_64|amd64)        GOARCH="amd64" ;;
    *) fail "Unsupported architecture: $ARCH" ;;
  esac
  ok "Architecture: $ARCH → linux/$GOARCH"

  stop_service

  step "Fetching $BINARY"
  # Match the exact artifact for this arch (anchored on the closing quote so
  # 'arm' does not match 'arm64').
  DOWNLOAD_URL=$(curl -sf "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null \
    | grep -oE "https://[^\"]+/${BINARY}-linux-${GOARCH}\"" | tr -d '"' | head -1 || true)

  if [ -n "$DOWNLOAD_URL" ]; then
    echo "  📦 Found release binary"
    (curl -sfL -o "$INSTALL_DIR/$BINARY" "$DOWNLOAD_URL") & spin $! "Downloading binary"
    chmod +x "$INSTALL_DIR/$BINARY"
  else
    echo "  📦 No release found — building from source"
    command -v git &>/dev/null || fail "git is required to build from source (sudo apt install git)"
    if ! command -v go &>/dev/null; then
      warn "Go not found — installing via apt"
      (apt-get update -qq && apt-get install -y -qq golang-go) & spin $! "Installing Go"
    fi
    command -v go &>/dev/null || fail "Go (>= the version in server/go.mod) is required to build from source"
    TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
    (git clone --depth 1 "https://github.com/$REPO.git" "$TMP/wtwlt" 2>/dev/null) & spin $! "Cloning repository"
    VERSION=$(git -C "$TMP/wtwlt" describe --tags --always 2>/dev/null || echo dev)
    (cd "$TMP/wtwlt/server" && go build -ldflags "-X main.version=$VERSION" -o "$INSTALL_DIR/$BINARY" .) & spin $! "Building binary"
    chmod +x "$INSTALL_DIR/$BINARY"
  fi
fi
ok "Binary installed to $INSTALL_DIR/$BINARY"

if "$INSTALL_DIR/$BINARY" version &>/dev/null; then
  ok "$("$INSTALL_DIR/$BINARY" version 2>&1)"
else
  warn "Binary version check failed — continuing anyway"
fi

# ── Mosquitto broker ────────────────────────────────────────────────
# Provision the broker the server (and the station) talk to. Anonymous by
# default; if WTWLT_MQTT_USER is set in .env, require matching credentials.
# Set WTWLT_SKIP_BROKER=1 to manage the broker yourself.
env_val() { grep -E "^$1=" "$INSTALL_DIR/.env" 2>/dev/null | head -1 | cut -d= -f2-; }

step "Setting up Mosquitto broker"
if [ "${WTWLT_SKIP_BROKER:-}" = "1" ]; then
  warn "WTWLT_SKIP_BROKER=1 — skipping broker setup (manage it yourself)"
elif ! command -v apt-get &>/dev/null; then
  warn "No apt-get here — install/configure a broker manually:"
  warn "  the server needs MQTT on port 1883 (see server/mosquitto/mosquitto.conf)"
else
  if ! command -v mosquitto &>/dev/null; then
    (apt-get update -qq && apt-get install -y -qq mosquitto mosquitto-clients) & spin $! "Installing mosquitto"
  else
    ok "mosquitto already installed"
  fi

  MQ_USER="$(env_val WTWLT_MQTT_USER)"
  MQ_PASS="$(env_val WTWLT_MQTT_PASS)"
  CONF="/etc/mosquitto/conf.d/wtwlt.conf"
  if [ -n "$MQ_USER" ]; then
    PW="/etc/mosquitto/wtwlt.passwd"
    mosquitto_passwd -b -c "$PW" "$MQ_USER" "$MQ_PASS"
    chown mosquitto:mosquitto "$PW" 2>/dev/null || true
    chmod 600 "$PW"
    cat > "$CONF" <<EOF
# managed by wtwlt install.sh
listener 1883
allow_anonymous false
password_file $PW
EOF
    ok "Broker configured with auth for user '$MQ_USER' (port 1883)"
    warn "Ensure the station's secrets.h MQTT_USER/MQTT_PASS match."
  else
    cat > "$CONF" <<EOF
# managed by wtwlt install.sh
listener 1883
allow_anonymous true
EOF
    ok "Broker configured (anonymous, port 1883)"
  fi
  systemctl enable mosquitto >/dev/null 2>&1 || true
  systemctl restart mosquitto
  ok "Mosquitto running"
fi

# ── systemd service ─────────────────────────────────────────────────
step "Setting up systemd service"
cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<EOF
[Unit]
Description=wtwlt weather station server
After=network-online.target mosquitto.service
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$INSTALL_DIR
EnvironmentFile=$INSTALL_DIR/.env
Environment="WTWLT_DATA_DIR=$INSTALL_DIR"
ExecStart=$INSTALL_DIR/$BINARY
Restart=always
RestartSec=10
StandardOutput=append:$INSTALL_DIR/wtwlt.log
StandardError=append:$INSTALL_DIR/wtwlt.log

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable "$SERVICE_NAME" >/dev/null 2>&1
ok "Service enabled"
systemctl restart "$SERVICE_NAME"
ok "Service started"

# ── done ────────────────────────────────────────────────────────────
IP=$(hostname -I 2>/dev/null | awk '{print $1}')
echo ""
echo "  ⛅ wtwlt is live!"
echo ""
[ -n "$IP" ] && echo "  Dashboard:  http://$IP:8080/"
echo ""
echo "  Commands:"
echo "    sudo systemctl status $SERVICE_NAME    # check status"
echo "    sudo systemctl restart $SERVICE_NAME   # restart"
echo "    tail -f $INSTALL_DIR/wtwlt.log         # view logs"
echo "    $INSTALL_DIR/$BINARY version           # show version"
echo ""
echo "  Edit $INSTALL_DIR/.env (broker/port/retention), then restart."
echo ""
