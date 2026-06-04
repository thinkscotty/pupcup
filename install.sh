#!/usr/bin/env bash
# install.sh — one-shot PupCup installer for a Raspberry Pi.
#
# Run it ON the Pi (over SSH), as root. The quickest path:
#
#     curl -fsSL https://raw.githubusercontent.com/thinkscotty/pupcup/main/install.sh \
#       | sudo bash -s -- --display gc9a01
#
# or download first so you can read it before running:
#
#     curl -fsSLO https://raw.githubusercontent.com/thinkscotty/pupcup/main/install.sh
#     sudo bash install.sh --display oled --timezone America/Chicago
#
# What it does, in order: provisions the OS (packages, I2C/SPI/RTC overlays,
# hardware groups), downloads the prebuilt release binary matching this Pi's
# architecture and verifies its checksum, creates the unprivileged `pupcup`
# service user + dirs + config + systemd unit, enables the service, and reboots
# so the buses come up and the daemon starts clean. Idempotent — safe to re-run.
set -euo pipefail

# ---- defaults (override with the flags below) ------------------------------
REPO="thinkscotty/pupcup"
VERSION="latest"            # a release tag like v1.0.0, or "latest"
DISPLAY_PANEL="gc9a01"      # gc9a01 (240x240 round SPI LCD) | oled (128x64 I2C)
TIMEZONE="America/New_York"
HOSTNAME_WANT="pupcup"
LOCAL_BINARY=""             # --binary <path>: install this instead of downloading
DO_REBOOT=1

usage() {
  cat <<'EOF'
PupCup installer — run on the Raspberry Pi as root (sudo).

  --display <gc9a01|oled>   panel you wired (default: gc9a01)
  --timezone <IANA tz>      e.g. America/Chicago (default: America/New_York)
  --hostname <name>         system + mDNS name (default: pupcup)
  --version <tag|latest>    release to install (default: latest)
  --repo <owner/name>       GitHub repo to fetch from (default: thinkscotty/pupcup)
  --binary <path>           install a local binary instead of downloading a release
  --no-reboot               provision but don't reboot (the buses still need one)
  -h, --help                show this help
EOF
}

die()  { echo "error: $*" >&2; exit 1; }
step() { echo; echo "==> $*"; }

# ---- parse flags -----------------------------------------------------------
while [ $# -gt 0 ]; do
  case "$1" in
    --display)   DISPLAY_PANEL="${2:?--display needs a value}"; shift 2 ;;
    --timezone)  TIMEZONE="${2:?--timezone needs a value}"; shift 2 ;;
    --hostname)  HOSTNAME_WANT="${2:?--hostname needs a value}"; shift 2 ;;
    --version)   VERSION="${2:?--version needs a value}"; shift 2 ;;
    --repo)      REPO="${2:?--repo needs a value}"; shift 2 ;;
    --binary)    LOCAL_BINARY="${2:?--binary needs a path}"; shift 2 ;;
    --no-reboot) DO_REBOOT=0; shift ;;
    -h|--help)   usage; exit 0 ;;
    *)           die "unknown argument: $1 (try --help)" ;;
  esac
done

[ "$(id -u)" -eq 0 ] || die "must run as root — pipe to 'sudo bash' or use 'sudo bash install.sh'."
case "$DISPLAY_PANEL" in gc9a01|oled) ;; *) die "--display must be gc9a01 or oled" ;; esac

# ---- pre-flight: architecture + board --------------------------------------
step "Pre-flight checks"
case "$(uname -m)" in
  aarch64|arm64) ASSET="pupcup-linux-arm64" ;;
  armv7l)        ASSET="pupcup-linux-armv7" ;;
  armv6l)        die "ARMv6 (original Pi Zero / Zero W) isn't covered by the prebuilt releases — build from source." ;;
  *)             die "unsupported architecture '$(uname -m)' — PupCup targets 64-bit (aarch64) or armv7 Raspberry Pi OS." ;;
esac
model="$(tr -d '\0' < /proc/device-tree/model 2>/dev/null || true)"
case "$model" in
  *Raspberry\ Pi*) echo "    board: $model" ;;
  *)               echo "    warning: '$model' doesn't look like a Raspberry Pi — continuing anyway." ;;
esac
echo "    arch:  $(uname -m) -> asset $ASSET"
echo "    panel: $DISPLAY_PANEL"

# ---- packages --------------------------------------------------------------
step "Installing packages"
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y rsync avahi-daemon i2c-tools gpiod spi-tools
systemctl enable --now avahi-daemon

# ---- /boot config.txt: enable the SPI/I2C buses ----------------------------
step "Enabling the display + NeoPixel buses"
if   [ -f /boot/firmware/config.txt ]; then CONFIG_TXT=/boot/firmware/config.txt
elif [ -f /boot/config.txt ];          then CONFIG_TXT=/boot/config.txt
else die "no config.txt in /boot/firmware or /boot — is this Raspberry Pi OS?"; fi
echo "    editing $CONFIG_TXT"

# SPI0 (NeoPixel) is always on. The GC9A01 adds SPI1 (aux) + core_freq pinning,
# whose clock derives from the core clock; the OLED variant uses I2C instead.
# (No RTC — this build keeps fake-hwclock as its offline time fallback.)
block="# >>> pupcup >>>  (managed by install.sh — edits between the markers are overwritten on re-run)
dtparam=spi=on"
if [ "$DISPLAY_PANEL" = gc9a01 ]; then
  block="$block
dtoverlay=spi1-1cs
core_freq=400
core_freq_min=400"
else
  block="$block
dtparam=i2c_arm=on"
fi
block="$block
# <<< pupcup <<<"

# Idempotent: remove any previous block, then append the current one (with a
# leading blank line so it never glues onto an existing entry).
sed -i '/# >>> pupcup >>>/,/# <<< pupcup <<</d' "$CONFIG_TXT"
printf '\n%s\n' "$block" >> "$CONFIG_TXT"

# OLED build only: the dtparam enables the I2C controller, but the userspace
# /dev/i2c-1 node (what the OLED driver + i2cdetect open) needs the i2c-dev
# module. The GC9A01 build uses no I2C, so skip it there.
if [ "$DISPLAY_PANEL" = oled ]; then
  echo i2c-dev > /etc/modules-load.d/i2c-dev.conf
fi

# ---- Timekeeping -----------------------------------------------------------
# No RTC on this build: NTP keeps time online, and fake-hwclock (shipped enabled
# on Raspberry Pi OS) restores the last-saved time offline. We intentionally
# KEEP fake-hwclock — feedings recorded before NTP sync are flagged
# "time unverified" in the web app and corrected there. Nothing to do here.

# ---- service user + directories --------------------------------------------
step "Creating the pupcup service user and directories"
useradd --system --home /var/lib/pupcup --shell /usr/sbin/nologin pupcup 2>/dev/null || true
for grp in gpio i2c spi; do
  usermod -aG "$grp" pupcup 2>/dev/null || echo "    (group '$grp' missing — skipped)"
done
# Let the human login account run the hardware probes/diagnostics without sudo.
if [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != root ]; then
  usermod -aG gpio,i2c,spi "$SUDO_USER" 2>/dev/null || true
fi
install -d -m 0755 /opt/pupcup
install -d -o pupcup -g pupcup -m 0755 /var/lib/pupcup /var/lib/pupcup/photos
install -d -m 0755 /etc/pupcup

# ---- fetch + install the binary --------------------------------------------
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
if [ -n "$LOCAL_BINARY" ]; then
  step "Installing local binary: $LOCAL_BINARY"
  [ -f "$LOCAL_BINARY" ] || die "no such file: $LOCAL_BINARY"
  cp "$LOCAL_BINARY" "$tmp/$ASSET"
else
  step "Downloading $ASSET ($VERSION) from $REPO"
  if [ "$VERSION" = latest ]; then
    base="https://github.com/$REPO/releases/latest/download"
  else
    base="https://github.com/$REPO/releases/download/$VERSION"
  fi
  curl -fsSL -o "$tmp/$ASSET"     "$base/$ASSET"     || die "download failed: $base/$ASSET"
  curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS" || die "download failed: $base/SHA256SUMS"
  echo "    verifying checksum"
  ( cd "$tmp" && grep " $ASSET\$" SHA256SUMS | sha256sum -c - ) \
    || die "checksum mismatch for $ASSET — refusing to install."
fi
install -m 0755 -o root -g root "$tmp/$ASSET" /opt/pupcup/pupcup
echo "    installed /opt/pupcup/pupcup"

# ---- config.yaml (never clobber an edited one) -----------------------------
step "Writing /etc/pupcup/config.yaml"
if [ -f /etc/pupcup/config.yaml ]; then
  echo "    exists — left untouched"
else
  # A short config is enough: the daemon starts from its built-in defaults and
  # overlays only what's set here. The full documented key list is in the repo's
  # deploy/config.example.yaml.
  cat > /etc/pupcup/config.yaml <<EOF
# PupCup configuration. The daemon starts from its built-in defaults and overlays
# the keys below; any field may also be overridden by a PUPCUP_<UPPER_SNAKE> env
# var. The full documented list lives in deploy/config.example.yaml in the repo.
listen: ":80"
db_path: "/var/lib/pupcup/pupcup.sqlite"
photo_dir: "/var/lib/pupcup/photos"
timezone: "$TIMEZONE"

# Panel this build drives: "gc9a01" (round SPI LCD) or "oled" (128x64 I2C).
display: "$DISPLAY_PANEL"

mdns_hostname: "$HOSTNAME_WANT"   # the dashboard is reachable at <name>.local
EOF
  echo "    wrote it (display=$DISPLAY_PANEL, timezone=$TIMEZONE)"
fi

# ---- systemd unit ----------------------------------------------------------
step "Installing the systemd service"
cat > /etc/systemd/system/pupcup.service <<'EOF'
[Unit]
Description=PupCup feeding tracker
Documentation=https://github.com/thinkscotty/pupcup
After=network-online.target avahi-daemon.service time-sync.target
Wants=network-online.target

[Service]
Type=notify
User=pupcup
Group=pupcup
# Hardware access: /dev/gpiochip0 (gpio), /dev/i2c-1 (i2c), /dev/spidev* (spi).
SupplementaryGroups=gpio i2c spi
ExecStart=/opt/pupcup/pupcup --config /etc/pupcup/config.yaml
Restart=on-failure
RestartSec=5
WatchdogSec=60
StateDirectory=pupcup
RuntimeDirectory=pupcup
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
NoNewPrivileges=true
ReadWritePaths=/var/lib/pupcup /etc/pupcup
# Binding :80 as a non-root user needs CAP_NET_BIND_SERVICE; nothing else.
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable pupcup >/dev/null
echo "    enabled (starts automatically on the next boot)"

# ---- hostname --------------------------------------------------------------
if [ "$(hostnamectl --static 2>/dev/null || true)" != "$HOSTNAME_WANT" ]; then
  step "Setting hostname to $HOSTNAME_WANT"
  hostnamectl set-hostname "$HOSTNAME_WANT" || echo "    (couldn't set hostname; continuing)"
fi

# ---- done ------------------------------------------------------------------
step "Install complete"
cat <<EOF

  PupCup ($VERSION) is installed and enabled. After the reboot:
    - the I2C/SPI buses and the RTC come up, then the service starts on its own
    - dashboard:  http://$HOSTNAME_WANT.local/   (or the Pi's IP address)
    - logs:       journalctl -u pupcup -f
    - config:     /etc/pupcup/config.yaml   (sudo systemctl restart pupcup after edits)

EOF
if [ "$DO_REBOOT" -eq 1 ]; then
  echo "  Rebooting in 5 s — Ctrl-C to cancel (then run 'sudo reboot' yourself when ready)."
  sleep 5
  reboot
else
  echo "  --no-reboot set: run 'sudo reboot' to finish — the buses need it before the service can start."
fi
