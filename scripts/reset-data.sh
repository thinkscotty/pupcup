#!/usr/bin/env bash
# reset-data.sh — clear feeding history + remove the 4-hour lock. RUN ON YOUR MAC.
#
# For quick test resets WITHOUT rebuilding or redeploying. Deletes feedings,
# snacks, and their add-in tag links, and clears the persisted post-meal lock,
# then restarts the service so the device boots back to the dog selector,
# unlocked, with no feedings.
#
# KEEPS: dogs, the add-in tag catalog (/tags), and illness/stress events.
# For a full wipe + rebuild + redeploy, use scripts/fresh-deploy.sh instead.
#
#   ./scripts/reset-data.sh
#   TARGET=scotty@pupcup.local ./scripts/reset-data.sh     # override the host
set -euo pipefail

TARGET=${TARGET:-scotty@192.168.0.141}

echo "==> Clearing feedings + lock on $TARGET"
ssh "$TARGET" 'bash -s' <<'REMOTE'
set -euo pipefail
DB=/var/lib/pupcup/pupcup.sqlite

if ! command -v sqlite3 >/dev/null 2>&1; then
  echo "  - installing sqlite3 (one time)"
  sudo apt-get update -qq && sudo apt-get install -y -qq sqlite3
fi

echo "  - stopping service (release the DB)"
sudo systemctl stop pupcup

echo "  - deleting feedings, snacks, tag links; clearing the lock"
# feeding_tags first: its ON DELETE CASCADE only fires with foreign_keys=ON,
# which the sqlite3 CLI leaves OFF — delete the link rows explicitly to be safe.
sudo -u pupcup sqlite3 "$DB" 'DELETE FROM feeding_tags; DELETE FROM feedings; DELETE FROM snacks; UPDATE device_state SET locked_until_utc=NULL, last_lock_reason=NULL WHERE id=1;'

echo "  - feedings remaining: $(sudo -u pupcup sqlite3 "$DB" 'SELECT COUNT(*) FROM feedings;')"
echo "  - lock locked_until_utc: [$(sudo -u pupcup sqlite3 "$DB" 'SELECT locked_until_utc FROM device_state WHERE id=1;')]  (empty = cleared)"

echo "  - starting service"
sudo systemctl start pupcup
sleep 1
echo "  - service is: $(sudo systemctl is-active pupcup)"
REMOTE

echo "==> Done. Device should be at the dog selector, unlocked, with no feedings."
