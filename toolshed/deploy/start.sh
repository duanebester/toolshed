#!/usr/bin/env bash
set -eu

# ---------------------------------------------------------------------------
# ToolShed entrypoint – starts Dolt SQL server + SSH server in one container
# ---------------------------------------------------------------------------

echo "==> Configuring Dolt identity..."
dolt config --global --add user.email 'toolshed@toolshed.sh'
dolt config --global --add user.name 'ToolShed'

# Persistent volume mount on Fly
DATA_DIR=/data/dolt

# ---------------------------------------------------------------------------
# Initialize the registry database (first deploy only)
# ---------------------------------------------------------------------------
if [ ! -d "$DATA_DIR/toolshed_registry/.dolt" ]; then
    echo "==> Initializing registry database..."
    mkdir -p "$DATA_DIR/toolshed_registry"
    cd "$DATA_DIR/toolshed_registry"
    dolt init --name 'ToolShed' --email 'toolshed@toolshed.sh'
    dolt sql < /schema/registry/001_init.sql
    dolt sql < /schema/registry/002_embeddings.sql
    dolt add .
    dolt commit -m "Initial registry schema"
    dolt sql < /schema/registry/seed.sql
    dolt add .
    dolt commit -m "Seed data"
    echo "==> Registry database initialised."
else
    echo "==> Registry database already exists, skipping init."
fi

# ---------------------------------------------------------------------------
# Initialize the ledger database (first deploy only)
# ---------------------------------------------------------------------------
if [ ! -d "$DATA_DIR/toolshed_ledger/.dolt" ]; then
    echo "==> Initializing ledger database..."
    mkdir -p "$DATA_DIR/toolshed_ledger"
    cd "$DATA_DIR/toolshed_ledger"
    dolt init --name 'ToolShed' --email 'toolshed@toolshed.sh'
    dolt sql < /schema/ledger/001_init.sql
    dolt add .
    dolt commit -m "Initial ledger schema"
    echo "==> Ledger database initialised."
else
    echo "==> Ledger database already exists, skipping init."
fi

# ---------------------------------------------------------------------------
# Create root@'%' user (connections from localhost within the container)
# ---------------------------------------------------------------------------
echo "==> Ensuring root@'%' user exists..."
dolt --data-dir "$DATA_DIR" sql -q "
    CREATE USER IF NOT EXISTS 'root'@'%' IDENTIFIED BY '';
    GRANT ALL PRIVILEGES ON *.* TO 'root'@'%' WITH GRANT OPTION;
" 2>/dev/null || true

# ---------------------------------------------------------------------------
# Start Dolt SQL server (background)
# ---------------------------------------------------------------------------
echo "==> Starting Dolt SQL server..."
dolt sql-server --host 0.0.0.0 --port 3306 --data-dir "$DATA_DIR" &
DOLT_PID=$!

# Clean up the Dolt process when the script exits
trap "kill $DOLT_PID 2>/dev/null || true" EXIT

# ---------------------------------------------------------------------------
# Wait for Dolt to become ready
# ---------------------------------------------------------------------------
echo "==> Waiting for Dolt..."
for i in $(seq 1 30); do
    if dolt --data-dir "$DATA_DIR" sql -q "SELECT 1" >/dev/null 2>&1; then
        echo "==> Dolt is ready."
        break
    fi
    sleep 1
done

# ---------------------------------------------------------------------------
# Apply pending schema migrations (idempotent)
# ---------------------------------------------------------------------------
# Each CREATE INDEX is wrapped with || true so it's a no-op if the index
# already exists (Dolt/MySQL returns "duplicate key name" which we ignore).
# The dolt commit is also wrapped — if nothing changed, "nothing to commit"
# is silently swallowed.
echo "==> Applying pending schema migrations..."
cd "$DATA_DIR/toolshed_registry"

# 003: upvote constraints — unique index + covering index for budget checks
dolt sql -q "CREATE UNIQUE INDEX idx_upvotes_key_invocation ON upvotes (key_fingerprint, invocation_id)" 2>/dev/null || true
dolt sql -q "CREATE INDEX idx_upvotes_key_tool ON upvotes (key_fingerprint, tool_id)" 2>/dev/null || true
dolt add . 2>/dev/null || true
dolt commit -m "Apply 003: upvote constraints" 2>/dev/null || true

echo "==> Schema migrations complete."

# ---------------------------------------------------------------------------
# Configure environment for the SSH server
# ---------------------------------------------------------------------------
echo "==> Setting SSH server environment variables..."
export TOOLSHED_SSH_PORT="${TOOLSHED_SSH_PORT:-2222}"
export TOOLSHED_HOST_KEY_PATH="/data/ssh/toolshed_host_key"
export TOOLSHED_REGISTRY_DSN="root@tcp(127.0.0.1:3306)/toolshed_registry?parseTime=true"
export TOOLSHED_LEDGER_DSN="root@tcp(127.0.0.1:3306)/toolshed_ledger?parseTime=true"
export TOOLSHED_WEB_PORT="${TOOLSHED_WEB_PORT:-8080}"
export TOOLSHED_WEB_ROOT="/public"
export TOOLSHED_MODEL_DIR="${TOOLSHED_MODEL_DIR:-/model}"
export TOOLSHED_ONNX_LIB="${TOOLSHED_ONNX_LIB:-/usr/local/lib/libonnxruntime.so}"

# ---------------------------------------------------------------------------
# SSH hardening — real client IPs + rate limiting + timeouts
#
# PROXY protocol is required on Fly.io so the Go server sees real client IPs
# instead of Fly's internal proxy addresses. The fly.toml must also have:
#   [services.proxy_proto_options]
#     version = "v2"
# ---------------------------------------------------------------------------
export TOOLSHED_PROXY_PROTOCOL="${TOOLSHED_PROXY_PROTOCOL:-true}"
export TOOLSHED_RATE_PER_IP="${TOOLSHED_RATE_PER_IP:-20}"       # max new conns per IP per minute
export TOOLSHED_MAX_PER_IP="${TOOLSHED_MAX_PER_IP:-10}"         # max concurrent conns per IP
export TOOLSHED_MAX_TOTAL="${TOOLSHED_MAX_TOTAL:-200}"          # max total concurrent conns
export TOOLSHED_BAN_AFTER="${TOOLSHED_BAN_AFTER:-5}"            # ban after N violations
export TOOLSHED_BAN_DURATION="${TOOLSHED_BAN_DURATION:-15m}"    # ban duration
export TOOLSHED_MAX_SESSION="${TOOLSHED_MAX_SESSION:-30m}"      # max session lifetime
export TOOLSHED_IDLE_TIMEOUT="${TOOLSHED_IDLE_TIMEOUT:-5m}"     # idle timeout
export TOOLSHED_MAX_AUTH_TRIES="${TOOLSHED_MAX_AUTH_TRIES:-3}"  # max auth attempts per conn

mkdir -p /data/ssh

# ---------------------------------------------------------------------------
# Start the SSH server (foreground, replaces this shell)
# ---------------------------------------------------------------------------
echo "==> Starting ToolShed SSH server on port ${TOOLSHED_SSH_PORT}..."
exec toolshed-ssh
