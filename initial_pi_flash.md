# PupCup — Initial Pi Flash & Software Verification

First-time install of the **PupCup application** onto the Raspberry Pi, plus the
step-by-step verification pass for the new software (build-plan
[§13 UAT](pupcup_build_plan.md)). This assumes hardware bring-up is already
complete and every peripheral has been confirmed working with the standalone
probes (see [hardware_test_setup.md](hardware_test_setup.md)). If you have just
re-flashed the SD card, do the OS provisioning in
[hardware_test_setup.md §2](hardware_test_setup.md) **first**, then come back
here.

The deployment is fully scripted in [deploy/](deploy/). This doc walks the
scripted path **and** spells out the equivalent manual commands (so the
ownership/permission story is explicit), then drives verification.

---

## 0. This device

| Fact            | Value                                            |
|-----------------|--------------------------------------------------|
| System hostname | `pupcuppi`                                        |
| IP address      | `192.168.0.189`                                   |
| SSH user        | `scotty` (the sudoer / bring-up admin account)    |
| Service user    | `pupcup` (system, `nologin`, created by bootstrap)|
| Binary path     | `/opt/pupcup/pupcup` (root-owned, `0755`)         |
| Config path     | `/etc/pupcup/config.yaml`                          |
| Data dir        | `/var/lib/pupcup/` (db + `photos/`, `pupcup`-owned)|
| HTTP listen     | `:80`                                              |

> **Two hostnames, don't conflate them.** SSH and deploy use the Pi's *system*
> hostname/IP — `scotty@192.168.0.189` (or `scotty@pupcuppi.local`). The web app
> separately advertises itself over mDNS as **`pupcup.local`** (the
> `mdns_hostname` default), and the OS's avahi advertises `pupcuppi.local`. So
> **before** the app is running, reach the Pi at `192.168.0.189` /
> `pupcuppi.local`; **once** it's running the dashboard is reachable at any of
> `http://192.168.0.189/`, `http://pupcuppi.local/`, or `http://pupcup.local/`.
> If you'd rather the app advertise `pupcuppi.local` too, set
> `mdns_hostname: pupcuppi` in the config (§2.4).

For brevity the commands below use this shell variable on the **laptop**:

```sh
export TARGET=scotty@192.168.0.189      # or scotty@pupcuppi.local
```

---

## 1. Prerequisites (confirm once, on the Pi)

The app needs I²C + SPI enabled, the device nodes present, the system clock
sane, and avahi running. If you followed [hardware_test_setup.md §2](hardware_test_setup.md)
these are already done — verify quickly:

```sh
ssh $TARGET
# I²C devices present (expect 0x3C OLED, and 0x68 or UU for the RTC):
sudo i2cdetect -y 1
# Device nodes exist:
ls -l /dev/i2c-1 /dev/spidev0.0 /dev/gpiochip0
# Clock is real + synced (RTC overlay + NTP):
timedatectl
```

If `i2cdetect`/`/dev/spidev0.0` are missing, I²C/SPI aren't enabled or the RTC
overlay isn't applied — fix that with
[hardware_test_setup.md §2.4–2.5](hardware_test_setup.md) before continuing; it
is an OS/kernel problem, not an app problem.

`rsync` is used by the deploy script — confirm it's installed on the Pi
(`which rsync`); `sudo apt install -y rsync` if not (or use the `scp` fallbacks
noted below).

> The app runs as the unprivileged **`pupcup`** user, not `scotty`. The
> `bootstrap.sh` step below adds `pupcup` to the `gpio`/`i2c`/`spi` groups and
> the systemd unit re-asserts them via `SupplementaryGroups`, so the service
> gets hardware access without root. You do **not** need Go on the Pi — the
> binary is cross-compiled on the laptop.

---

## 2. Build & transfer

### 2.1 (Recommended, on the laptop) Commit so the version stamp is meaningful

`deploy.sh` stamps the binary with `git rev-parse --short HEAD`. The build
compiles your **working tree**, but the embedded `version` label comes from the
last commit — commit first so the label matches what's running:

```sh
cd /Users/scotty/code/webapp_projects/pupcup
git status            # there are local edits at time of writing
# git add -A && git commit -m "…"   # optional but recommended before flashing
```

### 2.2 Put the install artifacts on the Pi and bootstrap (one time)

`bootstrap.sh` only needs the four files in [deploy/](deploy/). Copy that
directory over and run it on the Pi:

```sh
# from the repo root on the laptop:
rsync -avz deploy/ "$TARGET:~/pupcup-deploy/"
#   (scp fallback: scp -r deploy/* "$TARGET:~/pupcup-deploy/")

ssh $TARGET 'bash ~/pupcup-deploy/bootstrap.sh'
```

`bootstrap.sh` is idempotent and does all of this for you:

- creates the **`pupcup`** system user (`--system`, home `/var/lib/pupcup`,
  shell `nologin`) and adds it to `gpio`/`i2c`/`spi`;
- creates `/opt/pupcup` (root, `0755`), `/var/lib/pupcup` + `/var/lib/pupcup/photos`
  (`pupcup:pupcup`, `0755`), `/etc/pupcup` (root, `0755`);
- installs [deploy/config.example.yaml](deploy/config.example.yaml) →
  `/etc/pupcup/config.yaml` **only if absent** (never clobbers an edited config);
- installs [deploy/pupcup.service](deploy/pupcup.service) →
  `/etc/systemd/system/`, runs `daemon-reload`, and `enable`s the service
  (does **not** start it yet — that happens on first deploy).

> **Alternative:** `git clone git@github.com:thinkscotty/pupcup.git` on the Pi
> and run `./deploy/bootstrap.sh` from the checkout (needs git + repo access on
> the Pi). The rsync path above avoids that.

### 2.3 Adjust the config if needed (on the Pi)

The shipped config is identical to the built-in defaults (timezone
`America/New_York`, pins matching the wiring), so usually no edit is needed.
Edit only if a default doesn't fit:

```sh
sudo nano /etc/pupcup/config.yaml
sudo systemctl restart pupcup    # only if you edit after the first deploy
```

Common edits: `timezone`, or `mdns_hostname: pupcuppi` to unify the advertised
name with the system hostname. Every key is documented inline and may also be
overridden by a `PUPCUP_<UPPER_SNAKE>` env var.

### 2.4 Build, ship, install, restart (from the laptop)

**Scripted (recommended):**

```sh
cd /Users/scotty/code/webapp_projects/pupcup
TARGET=$TARGET ./deploy/deploy.sh
```

That single command cross-compiles `linux/arm64`, rsyncs the binary to
`/tmp/pupcup.new` on the Pi, `sudo install`s it root-owned to
`/opt/pupcup/pupcup`, restarts the service, and prints `systemctl status`.

**Manual equivalent** (what `deploy.sh` runs under the hood — use this if you
want to see each step / the ownership changes explicitly):

```sh
# 1. Cross-compile a stripped, version-stamped, static arm64 binary on the laptop:
cd /Users/scotty/code/webapp_projects/pupcup
VERSION=$(git rev-parse --short HEAD)
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
  go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
  -o build/pupcup ./cmd/pupcup

# 2. Ship it to a scratch path on the Pi:
rsync -avz --progress build/pupcup "$TARGET:/tmp/pupcup.new"
#   (scp fallback: scp build/pupcup "$TARGET:/tmp/pupcup.new")

# 3. Install root-owned 0755, then (re)start the service — note the ownership/mode:
ssh "$TARGET" 'sudo install -m 0755 -o root -g root /tmp/pupcup.new /opt/pupcup/pupcup \
  && rm -f /tmp/pupcup.new \
  && sudo systemctl restart pupcup \
  && sleep 1 \
  && sudo systemctl status pupcup --no-pager --lines=10'
```

For every later update, repeat **only §2.4** — `bootstrap.sh` is a one-time step.

---

## 3. Initial launch

The deploy in §2.4 already started the service. Confirm a clean first launch:

```sh
ssh $TARGET

# Service is up and signalled READY (the unit is Type=notify):
sudo systemctl status pupcup --no-pager
#   expect: Active: active (running);  no Restart= thrash

# Structured JSON logs — watch them live:
journalctl -u pupcup -f
#   expect lines like: "pupcup starting" → "seeded dogs" (first boot only)
#   → "device ready" with "listen":":80". Ctrl-C to stop following.

# The service user can actually reach the hardware nodes:
sudo -u pupcup test -r /dev/i2c-1 \
  && sudo -u pupcup test -r /dev/gpiochip0 \
  && sudo -u pupcup test -r /dev/spidev0.0 \
  && echo "device nodes OK"
```

On the **device** itself you should see the boot splash ("PupCup", ~1.5 s) give
way to the dog-selector scene on the OLED, and the LED bar idle (off). From a
phone/laptop on the same wifi, open `http://192.168.0.189/` (or
`http://pupcup.local/`) — the dashboard should load with the seeded dogs.

### Running in the foreground for debugging

If the service won't come up and the journal isn't enough, stop it and run the
binary directly to see logs on the terminal. Binding `:80` needs
`CAP_NET_BIND_SERVICE` (which the service has but a bare shell does not), so use
a high port for a manual run:

```sh
sudo systemctl stop pupcup
sudo -u pupcup env PUPCUP_LISTEN=:8080 /opt/pupcup/pupcup --config /etc/pupcup/config.yaml
#   browse http://192.168.0.189:8080/ ; Ctrl-C to quit
sudo systemctl start pupcup    # restore the real service when done
```

(A config error makes the daemon **fail fast** with a clear `invalid config:`
message — that path surfaces it immediately.)

---

## 4. Verification / UAT

Adapted from [pupcup_build_plan.md §13](pupcup_build_plan.md). Run every item on
the **deployed device**. Check them off as you go.

### 4.1 Install & service health
- [ ] `systemctl status pupcup` shows `active (running)`, no restart thrash.
- [ ] `journalctl -u pupcup` shows structured JSON logs ending in `device ready`.
- [ ] First boot logged `seeded dogs` once; a restart does **not** re-seed.
- [ ] `sudo -u pupcup` can read `/dev/i2c-1`, `/dev/gpiochip0`, `/dev/spidev0.0`.
- [ ] `/opt/pupcup/pupcup` is `root:root 0755`; `/var/lib/pupcup` is `pupcup:pupcup`.

### 4.2 Cold-boot
- [ ] Power-cycle the Pi cold. Within ~30 s the OLED shows the dog selector.
- [ ] `192.168.0.189` (and `pupcup.local`) loads the dashboard from a phone.
- [ ] LED bar is off while idle.

### 4.3 Button-driven feeding (the device)
- [ ] Rotate the dial — OLED cycles through dogs; wraps at the ends.
- [ ] Select dog A, tap **GREEN** — OLED confirms; dashboard shows A fed **full**.
- [ ] Select dog B, tap **YELLOW** — **partial** recorded.
- [ ] Select dog C, tap **RED** — **none** recorded.
- [ ] After the last dog's meal the LED turns **solid green** and the 4 h lock is
      timed **from that last meal**.
- [ ] OLED transitions to the locked-summary scene listing each dog's score.
- [ ] Grace timeout: feed only 2 of 3 dogs, wait `meal_complete_grace_minutes`
      (15) — device locks with the two; the third has no record (add later on web).
- [ ] Add-in chord: select a dog, **hold GREEN**, tap **BLUE** while holding —
      OLED shows the add-in picker ranked by that dog's history with an
      "Other (name later)" row last. Dial to scroll, short-press the rotary on a
      tag — meal commits with exactly that tag, advances to the next dog.
- [ ] Reverse chord: **hold BLUE**, then tap GREEN/YELLOW/RED — same picker;
      selecting a tag commits the meal with it.
- [ ] Add-in "Other": pick "Other" — meal commits and later shows on `/feedings`
      flagged "name this add-in".
- [ ] Add-in walk-away: start the chord, do nothing for `addin_idle_seconds`
      (30) — pending meal commits **untagged** (not lost) and advances.
- [ ] A plain quick tap (press + release, no Blue) commits a normal meal with no
      perceptible delay.
- [ ] During the lock, taps on G/Y/R are ignored (silent).
- [ ] **BLUE** tap alone in the locked period is ignored; **press-and-hold BLUE
      (≥ 1.5 s)** enters snack mode → pick a dog → tap BLUE → snack recorded and
      a `*` appears next to that dog in the summary; snack mode exits after 60 s
      idle back to the locked summary.
- [ ] Long-press the **rotary SW (≥ 1.5 s)** clears the lock; LED fades out;
      OLED returns to the selector.
- [ ] After 4 h the lock auto-clears on its own.

### 4.4 Web app
- [ ] Dashboard shows each dog's **last-fed time + score** accurately.
- [ ] Edit a past feeding (timestamp + score) — reflected in `/history` and the
      dog's detail chart (`/dogs/{id}`).
- [ ] Soft-delete a feeding — it leaves the table; the chart updates.
- [ ] Add a retroactive feeding with a custom timestamp — lands in the right
      chronological spot.
- [ ] On `/feedings`, add two add-in tags to one meal via chips — both persist
      and show on the dog's history; remove one — it detaches, the other stays.
- [ ] Create a brand-new tag from the create-on-the-fly field — reusable on the
      next feeding and visible in `/tags`.
- [ ] Resolve a device "Other" feeding: open the flagged one, replace Unspecified
      with a real tag — the flag clears.
- [ ] Archive a tag in `/tags` — gone from new pickers, still shown on past
      feedings that used it.
- [ ] Per-dog tag ranking: a tag used often for dog A sorts above unused tags in
      A's picker (device + web), and that ranking is **not** mirrored to dog B.
- [ ] Add an illness event spanning yesterday→today with "ongoing"; later set the
      end date.
- [ ] Add a stress event for the whole household.
- [ ] Manage dogs: rename, change accent color, upload a photo (appears on the
      dashboard); an over-limit image (>150 KB or >320×320) is rejected with a
      clear message.

### 4.5 Resilience
- [ ] Reboot with home wifi off, press a button — feeding recorded with a sane
      timestamp from the DS1307 RTC. Re-enable wifi — `pupcup.local` reachable and
      the entry appears.
- [ ] Re-run `./deploy/deploy.sh` while the device is **locked** — service
      restarts, OLED briefly blanks, LED bar resumes green, lock state is
      preserved (confirm via dashboard).
- [ ] Pull power abruptly mid-write — on next boot no DB corruption (SQLite WAL)
      and the state machine resumes. (A feeding pressed in the final instant
      before the cut may be lost — `synchronous=NORMAL`, the accepted stance —
      and is re-addable on the web.)
- [ ] `systemctl status pupcup` still `active (running)`; journald has the logs.

### 4.6 Polish
- [ ] Dashboard renders cleanly on iPhone Safari and a desktop browser.
- [ ] No perceptible input lag (< 100 ms button-to-OLED).
- [ ] No spurious feedings from button bounce after a vigorous press.
- [ ] OLED text is legible from across the room.

**When every box is checked, v1 is shippable.**

---

## 5. Troubleshooting

| Symptom | First thing to check |
|---|---|
| Service won't start / restart-loops | `journalctl -u pupcup -e` — a config error fails fast with `invalid config:`. Run foreground (§3) to see it plainly. |
| `bind: permission denied` on `:80` | Manual foreground run lacks `CAP_NET_BIND_SERVICE` — use `PUPCUP_LISTEN=:8080`, or run via systemd (which has the cap). |
| `bind: address already in use` | Something else holds `:80` (another `pupcup`? a web server?). `sudo ss -ltnp 'sport = :80'`. |
| Hardware errors at startup | `sudo -u pupcup test -r /dev/i2c-1 …` (§3); re-check group membership (`groups pupcup`) and that I²C/SPI are enabled. |
| OLED blank | `sudo i2cdetect -y 1` must show `0x3C`; if absent it's wiring/kernel, not the app. |
| `pupcup.local` won't resolve | avahi up? (`systemctl status avahi-daemon`). Fall back to `192.168.0.189`. The app's own mDNS only advertises while it's running. |
| Wrong timestamps | `timedatectl` / `sudo hwclock -r`; confirm `timezone` in the config and the RTC overlay. |
| DB write errors | `/var/lib/pupcup` must be `pupcup:pupcup` and writable; the unit's `StateDirectory` should keep it so. |

Useful one-liners:

```sh
sudo systemctl status pupcup --no-pager
journalctl -u pupcup -f                 # follow live
journalctl -u pupcup -b                 # this boot
sudo systemctl restart pupcup
curl -fsS http://localhost/healthz && echo OK   # liveness probe (run on the Pi)
```

---

## 6. Sign-off

- [ ] §2 bootstrap + first deploy completed; service enabled and running.
- [ ] §3 initial launch clean (READY, logs, device nodes, OLED, web).
- [ ] §4.1–4.6 UAT all checked.

Once signed off, record the result in
[pupcup_build_plan.md §13](pupcup_build_plan.md) (milestone 15) and v1 is ready
for household use.
