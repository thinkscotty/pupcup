#!/usr/bin/env bash
# Cross-compile for the Pi (linux/arm64, pure Go), ship the binary, and restart
# the service. Run from the repo root on your laptop after ./deploy/bootstrap.sh
# has been run once on the Pi:
#
#     ./deploy/deploy.sh
#     TARGET=scotty@raspberrypi.local ./deploy/deploy.sh   # override the host
#
# TARGET is the SSH login of a sudoer on the Pi (the bring-up admin account) —
# NOT the pupcup service user, which is nologin. The binary is installed
# root-owned to /opt/pupcup/pupcup and runs as the pupcup user via systemd.
set -euo pipefail

cd "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

TARGET=${TARGET:-scotty@pupcup.local}
VERSION=$(git rev-parse --short HEAD 2>/dev/null || echo dev)

echo "==> Building build/pupcup (linux/arm64, version=$VERSION)"
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
  go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
  -o build/pupcup ./cmd/pupcup

echo "==> Shipping to $TARGET"
rsync -avz --progress build/pupcup "$TARGET:/tmp/pupcup.new"

echo "==> Installing + restarting on $TARGET"
ssh "$TARGET" 'sudo install -m 0755 -o root -g root /tmp/pupcup.new /opt/pupcup/pupcup \
  && rm -f /tmp/pupcup.new \
  && sudo systemctl restart pupcup \
  && sleep 1 \
  && sudo systemctl status pupcup --no-pager --lines=10'

echo "==> Deployed $VERSION."
