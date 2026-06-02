#!/usr/bin/env bash
# First-time install on a fresh Pi. Run once, from the repo root, as a sudoer
# (the bring-up admin account — NOT the pupcup service user, which is nologin):
#
#     ssh scotty@pupcup.local              # or run locally on the Pi
#     git clone … && cd pupcup
#     ./deploy/bootstrap.sh
#
# Idempotent: re-running it is safe. It creates the service user, the data and
# config dirs, installs the config (without clobbering an edited one) and the
# systemd unit, and enables the service. It does NOT ship the binary or start
# the service — run ./deploy/deploy.sh from your laptop for that.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ $EUID -eq 0 ]]; then
  SUDO=""
else
  SUDO="sudo"
fi

echo "==> Creating the pupcup system user + groups"
$SUDO useradd --system --home /var/lib/pupcup --shell /usr/sbin/nologin pupcup 2>/dev/null || true
# gpio/i2c/spi are created by the Pi OS; tolerate a missing one on a bare image.
for grp in gpio i2c spi; do
  $SUDO usermod -aG "$grp" pupcup 2>/dev/null || echo "    (group '$grp' missing — skipped)"
done

echo "==> Creating directories"
$SUDO install -d -m 0755 /opt/pupcup
$SUDO install -d -o pupcup -g pupcup -m 0755 /var/lib/pupcup /var/lib/pupcup/photos
$SUDO install -d -m 0755 /etc/pupcup

echo "==> Installing config (preserving any existing /etc/pupcup/config.yaml)"
if [[ -f /etc/pupcup/config.yaml ]]; then
  echo "    /etc/pupcup/config.yaml exists — left untouched"
else
  $SUDO install -m 0644 "$here/config.example.yaml" /etc/pupcup/config.yaml
fi

echo "==> Installing systemd unit"
$SUDO install -m 0644 "$here/pupcup.service" /etc/systemd/system/pupcup.service
$SUDO systemctl daemon-reload
$SUDO systemctl enable pupcup

cat <<'EOF'

==> Bootstrap complete.

Next:
  1. Edit /etc/pupcup/config.yaml if the defaults need changing (timezone, etc.).
  2. From your laptop, ship the binary and start the service:
         ./deploy/deploy.sh
  3. Verify hardware nodes are reachable as the service user:
         sudo -u pupcup test -r /dev/i2c-1 \
           && sudo -u pupcup test -r /dev/gpiochip0 \
           && sudo -u pupcup test -r /dev/spidev0.0 \
           && echo "device nodes OK"
EOF
