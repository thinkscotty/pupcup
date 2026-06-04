#!/usr/bin/env bash
# fresh-deploy.sh — FULL reset of the Pi's PupCup instance. RUN ON YOUR MAC.
#
# Builds the latest code (linux/arm64), wipes the device's data (database +
# photos), installs the fresh binary, and starts the service clean — so the
# daemon comes up with an empty DB and re-seeds the household's dogs.
#
#   ./scripts/fresh-deploy.sh
#   TARGET=scotty@pupcup.local ./scripts/fresh-deploy.sh    # override the host
#
# Prerequisite: deploy/bootstrap.sh has been run once on the Pi (it creates the
# pupcup user, /opt/pupcup, /etc/pupcup/config.yaml, and the systemd unit). This
# script PRESERVES those — it only resets the running instance + its data. For a
# lighter reset (clear feedings + lock, no rebuild) use scripts/reset-data.sh.
set -euo pipefail

cd "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

TARGET=${TARGET:-scotty@192.168.0.141}
VERSION=$(git rev-parse --short HEAD 2>/dev/null || echo dev)

command -v go >/dev/null 2>&1 || {
  echo "error: 'go' not found on PATH — run this on your Mac, not the Pi." >&2
  exit 1
}

echo "==> Building build/pupcup (linux/arm64, version=$VERSION)"
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
  go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
  -o build/pupcup ./cmd/pupcup

echo "==> Shipping binary to $TARGET"
scp build/pupcup "$TARGET:/tmp/pupcup.new"

echo "==> Wiping old instance + installing fresh on $TARGET"
ssh "$TARGET" 'bash -s' <<'REMOTE'
set -euo pipefail
echo "  - stopping service"
sudo systemctl stop pupcup || true
echo "  - erasing data dir (db, wal/shm, photos) — systemd + app recreate it on start"
sudo rm -rf /var/lib/pupcup
echo "  - installing new binary (root:root 0755)"
sudo install -m 0755 -o root -g root /tmp/pupcup.new /opt/pupcup/pupcup
rm -f /tmp/pupcup.new
echo "  - starting service"
sudo systemctl start pupcup
sleep 1
sudo systemctl status pupcup --no-pager --lines=10
REMOTE

echo "==> Fresh instance $VERSION deployed and running on $TARGET."
echo "    Dashboard: http://192.168.0.141/  (or http://pupcup.local/)"
