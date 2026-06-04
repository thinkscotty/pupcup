# PupCup — Initial Pi Setup & First Deploy

How to take a **freshly-flashed Raspberry Pi 3B+** and get it fully prepared to
run the PupCup application, then push the app onto it for the first time.

The philosophy of this doc: **do the work on the Pi, over SSH.** You flash a bare
OS image once from the laptop, then `ssh` in and provision the whole environment
from the Pi side — package installs, kernel/overlay config, hardware
permissions, the service user, and the config file. The **only** thing that stays
on the laptop is cross-compiling the Go binary and shipping it (the Pi has no Go
toolchain by design). Everything else is a Pi-side command.

This assumes the hardware is built and the peripherals have already been
confirmed with the standalone probes
([../hardware_test_setup.md](../hardware_test_setup.md)). If you have *not* done
that yet, do it first — this doc is about the software environment, not wiring.

---

## 0. This device

| Fact            | Value                                              |
|-----------------|----------------------------------------------------|
| System hostname | `pupcup`                                            |
| IP address      | `192.168.0.141`                                     |
| SSH user        | `scotty` (the sudoer / bring-up admin account)      |
| Service user    | `pupcup` (system, `nologin`, created by bootstrap)  |
| Display         | GC9A01 round LCD (default); OLED is a config option |
| Binary path     | `/opt/pupcup/pupcup` (root-owned, `0755`)           |
| Config path     | `/etc/pupcup/config.yaml`                            |
| Data dir        | `/var/lib/pupcup/` (db + `photos/`, `pupcup`-owned) |
| HTTP listen     | `:80`                                               |

> **Hostname.** The Pi's *system* hostname is `pupcup`, matching the web app's
> `mdns_hostname` default — so the same name resolves before and after the app
> starts. SSH in with `scotty@pupcup.local` (or `scotty@192.168.0.141`). Avahi
> advertises `pupcup.local`; once the app is up it advertises the same name, so
> the dashboard lands at `http://pupcup.local/` or `http://192.168.0.141/`.

---

## 1. Flash the OS (the one laptop step)

This is the only part that happens on the Mac — you need the imager to write the
SD card. Configure as much as possible *now* so the Pi comes up headless and
SSH-reachable, and you never need a keyboard/monitor on it.

1. Open **Raspberry Pi Imager** (https://www.raspberrypi.com/software/).
2. **Choose OS** → *Other general-purpose OS* → **Raspberry Pi OS Lite (64-bit)**
   (Debian 13 "Trixie" base). Lite is correct — no desktop needed.
3. Click the gear / **Edit Settings** and set:
   - **Hostname**: `pupcup`
   - **Username**: `scotty` · **Password**: set one
   - **Enable SSH** → paste your laptop's **public key** (passwordless login)
   - **Configure wireless LAN**: your SSID + password + country
   - **Locale**: timezone `America/New_York`, keyboard `us`
4. Write the card, eject, insert into the Pi, apply 5 V power.
5. Wait ~60 s for first boot, then confirm it's on the network:

   ```sh
   ssh scotty@pupcup.local
   ```

   If mDNS doesn't resolve (some Android phones / corporate wifi struggle), find
   the Pi's IP on your router admin page and `ssh scotty@<ip>` instead.

**From here on, every command runs on the Pi unless it explicitly says "on the
laptop".** SSH in and stay in.

---

## 2. Prepare the environment (all on the Pi)

This is the heart of the setup. Do these in order; the reboot at the end applies
the kernel changes.

### 2.1 Update the OS

```sh
sudo apt update && sudo apt full-upgrade -y
```

### 2.2 Install the packages PupCup needs

```sh
sudo apt install -y git rsync i2c-tools gpiod spi-tools avahi-daemon
sudo systemctl enable --now avahi-daemon
```

What each is for:

| Package        | Why                                                            |
|----------------|---------------------------------------------------------------|
| `git`          | clone this repo on the Pi (bootstrap + config files live in it)|
| `rsync`        | how the laptop's `deploy.sh` ships the binary                  |
| `i2c-tools`    | `i2cdetect` — bus scan (only needed for the OLED variant)      |
| `gpiod`        | `gpioget`/`gpiomon` — raw GPIO checks for buttons/rotary       |
| `spi-tools`    | `spi-pipe` — SPI sanity checks for the LCD/NeoPixel buses      |
| `avahi-daemon` | keeps `pupcup.local` resolvable before/after the app starts    |

> The app itself needs **no** Go runtime, no libraries — it's a single static
> binary cross-compiled on the laptop. These packages are for provisioning and
> diagnostics only. (`sqlite3` is optional; `scripts/reset-data.sh` installs it
> on demand if you ever want to poke the DB.)

### 2.3 Enable the SPI buses (`/boot/firmware/config.txt`)

The reference (GC9A01) build uses **two** SPI buses and no I²C:

- **SPI0** (`/dev/spidev0.0`) → SK6812 NeoPixel bar (via the 74AHCT125 shifter)
- **SPI1** (`/dev/spidev1.0`) → GC9A01 round LCD

Open the config and append the PupCup block at the very end of the file:

```sh
sudo nano /boot/firmware/config.txt
```

```ini
# --- PupCup hardware ---
dtparam=spi=on
dtoverlay=spi1-1cs
core_freq=400
core_freq_min=400
```

> ⚠️ **Two config.txt footguns that silently break this:**
> 1. **No inline comments.** `config.txt` is not a shell file — the firmware
>    reads everything after `=` to the end of the line as the value. A trailing
>    `# comment` becomes part of the value and breaks the directive; worse, a
>    comma in that comment is parsed as a `dtoverlay` parameter separator and
>    makes the overlay fail outright. Keep every `#` comment on its **own line**.
> 2. **Put it under `[all]` (or no filter).** `config.txt` has conditional
>    `[pi4]` / `[cm4]` / `[all]` section headers; anything below a `[pi4]`-style
>    header is ignored on a Pi 3. Append at the very end so the block lands under
>    the trailing `[all]` section.

`core_freq` / `core_freq_min` are **required for the GC9A01**: SPI1's clock is
derived from the core clock, so letting the core scale would make the LCD bus
timing drift.

> **OLED variant instead?** The SSD1306 is on I²C, not SPI1. Drop the `spi1-1cs`
> and `core_freq` lines and use this block instead:
>
> ```ini
> # --- PupCup hardware (OLED variant) ---
> dtparam=spi=on
> dtparam=i2c_arm=on
> ```
>
> Then load the `i2c-dev` module so the userspace `/dev/i2c-1` node (which the
> OLED driver and `i2cdetect` open) actually appears — `dtparam=i2c_arm=on` only
> enables the controller:
>
> ```sh
> echo i2c-dev | sudo tee /etc/modules-load.d/i2c-dev.conf
> ```

### 2.4 Timekeeping (no RTC — nothing to configure)

This build has **no battery-backed RTC** (a deliberate choice — bulk and cost
for a rare, human-correctable failure). On Raspberry Pi OS, `systemd-timesyncd`
handles both halves of the job, so there is **nothing to set up here**:

- **online** → it syncs the clock over NTP;
- **offline** → it persists the time to `/var/lib/systemd/timesync/clock` and, on
  boot, advances the system clock to at least that saved timestamp *before* the
  network comes up — so a cold boot never starts at 1970.

> **Don't try to use `fake-hwclock`.** Raspberry Pi OS ships its service masked
> (`/usr/lib/systemd/system/fake-hwclock.service → /dev/null`, package-owned) on
> purpose, because `systemd-timesyncd` replaces it. You can't `enable` it and
> don't need to — `unmask` won't help (the mask is in the vendor dir).

If the Pi ever boots offline (a power cut that also took down the router), the
clock is restored to that last-saved time — close, but not exact — so any
feeding recorded before NTP re-syncs is flagged **"time unverified"** in the web
app. No data is lost; just confirm or correct the time later, which clears the
flag. (§3 verifies `timedatectl` shows the clock synced.)

### 2.5 Give your login (`scotty`) hardware access

So you can run the probes and diagnostics without `sudo`:

```sh
sudo usermod -aG gpio,i2c,spi scotty
```

(The unprivileged **`pupcup`** service user gets its own group membership in
§4 — this line is just for your interactive `scotty` session.)

### 2.6 Reboot to apply the kernel changes

```sh
sudo reboot
```

---

## 3. Verify the environment (on the Pi)

SSH back in after the reboot and confirm the platform is sound **before**
involving the app. A failure here is an OS/wiring problem, not an app problem.

```sh
ssh scotty@pupcup.local

# Group membership took effect:
groups                       # expect: scotty … gpio i2c spi

# Clock is synced over NTP:
timedatectl                  # expect: "System clock synchronized: yes"

# Every device node the app will open exists (LCD build):
ls -l /dev/gpiochip0 /dev/spidev0.0 /dev/spidev1.0
#   OLED build instead: ls -l /dev/gpiochip0 /dev/spidev0.0 /dev/i2c-1
#   and `sudo i2cdetect -y 1` should show 0x3C.
```

Expected nodes by build:

| Node              | Used for                          | LCD build | OLED build |
|-------------------|-----------------------------------|:---------:|:----------:|
| `/dev/gpiochip0`  | buttons, rotary, LCD DC/RST       |     ✓     |     ✓      |
| `/dev/spidev0.0`  | NeoPixel bar                      |     ✓     |     ✓      |
| `/dev/spidev1.0`  | GC9A01 LCD                        |     ✓     |     —      |
| `/dev/i2c-1`      | SSD1306 OLED panel                |     —     |     ✓      |

If `/dev/spidev1.0` is missing → the `spi1-1cs` overlay didn't apply (re-check
§2.3 — the inline-comment and `[all]`-section footguns are the usual causes). On
the OLED build, if `i2cdetect` shows no `0x3C` it's panel wiring/kernel; see
[pupcup_hardware_build.md](pupcup_hardware_build.md).

> **Optional but recommended:** before installing the app, re-run the standalone
> hardware probes one more time on this freshly-provisioned OS
> ([../hardware_test_setup.md §5](../hardware_test_setup.md)). It isolates "the
> board still works" from "the app works", which makes the §6 launch much easier
> to reason about.

---

## 4. Install the application (on the Pi)

Get the repo onto the Pi so the bootstrap script and config template are local —
no need to copy anything from the laptop.

### 4.1 Clone the repo

```sh
cd ~
git clone https://github.com/thinkscotty/pupcup.git
cd pupcup
```

(If the repo is private and you'd rather not set up Git auth on the Pi, the
fallback is to `rsync -avz deploy/ scotty@pupcup.local:~/pupcup-deploy/` from the
laptop and run `bootstrap.sh` out of that directory instead — bootstrap only
needs the four files in `deploy/`.)

### 4.2 Run the bootstrap (one time)

```sh
./deploy/bootstrap.sh
```

`bootstrap.sh` is idempotent — re-running it is safe. It:

- creates the **`pupcup`** system user (`--system`, home `/var/lib/pupcup`,
  shell `nologin`) and adds it to `gpio`/`i2c`/`spi`;
- creates `/opt/pupcup` (root, `0755`), `/var/lib/pupcup` + `…/photos`
  (`pupcup:pupcup`, `0755`), and `/etc/pupcup` (root, `0755`);
- installs [../deploy/config.example.yaml](../deploy/config.example.yaml) →
  `/etc/pupcup/config.yaml` **only if absent** (never clobbers an edited config);
- installs [../deploy/pupcup.service](../deploy/pupcup.service) →
  `/etc/systemd/system/`, runs `daemon-reload`, and `enable`s the service. It
  does **not** start it yet — that happens on the first deploy in §5, once the
  binary exists.

The systemd unit runs the app as the unprivileged `pupcup` user, re-asserts the
`gpio i2c spi` groups via `SupplementaryGroups`, and grants `CAP_NET_BIND_SERVICE`
so it can bind `:80` without root.

### 4.3 Adjust the config if needed

The shipped config matches the built-in defaults, so usually **no edit is
needed**. Open it only if a default doesn't fit:

```sh
sudo nano /etc/pupcup/config.yaml
```

The things you might actually change:

- **`display:`** — `gc9a01` (default, the round LCD) or `oled` (the 128×64 mono
  SSD1306 variant). This must match the panel you actually wired.
- **`timezone:`** — IANA name; drives the household-local day boundary.

Pins are **not** in this file — button/rotary/LCD GPIO assignments come from the
binary's built-in defaults
([../internal/config/config.go](../internal/config/config.go)), so a pin
correction ships with a normal deploy instead of a hand-edit. Every key is
documented inline and may be overridden by a `PUPCUP_<UPPER_SNAKE>` env var. If
you edit the config *after* the service is already running, apply it with
`sudo systemctl restart pupcup`.

---

## 5. Flash the code (from the laptop)

This is the lone laptop-side step in the software flow: the Pi has no Go
toolchain, so the binary is cross-compiled on the Mac and shipped over SSH.

```sh
# on the laptop, in the repo root:
cd /Users/scotty/code/webapp_projects/pupcup

# (recommended) commit first so the embedded version label is meaningful —
# deploy.sh stamps the binary with `git rev-parse --short HEAD`:
git status

TARGET=scotty@pupcup.local ./deploy/deploy.sh
```

`deploy.sh` cross-compiles a stripped, static `linux/arm64` binary, `rsync`s it
to `/tmp/pupcup.new` on the Pi, `sudo install`s it root-owned `0755` to
`/opt/pupcup/pupcup`, restarts the service, and prints `systemctl status`.
`TARGET` defaults to `scotty@pupcup.local`, so you can omit it on your network.

> **First-time / clean-slate variant.** `scripts/fresh-deploy.sh` does the same
> build + ship but **wipes `/var/lib/pupcup` first**, so the daemon comes up with
> an empty DB and re-seeds the household's dogs. Use it for the very first flash
> or any time you want a clean database; use plain `deploy.sh` for every routine
> update thereafter. (`bootstrap.sh` is a one-time step — never repeat it.)

For every later update, the whole loop is just: commit on the laptop →
`./deploy/deploy.sh`.

---

## 6. First launch & smoke test (on the Pi)

The deploy in §5 already started the service. Confirm a clean first boot:

```sh
ssh scotty@pupcup.local

# Service is up and signalled READY (the unit is Type=notify):
sudo systemctl status pupcup --no-pager
#   expect: Active: active (running);  no Restart= thrash

# Structured JSON logs, live:
journalctl -u pupcup -f
#   first boot: "pupcup starting" → "seeded dogs" → "device ready" with
#   "listen":":80". A later restart does NOT re-seed. Ctrl-C to stop following.

# The service user can actually reach the hardware nodes it needs:
sudo -u pupcup test -r /dev/gpiochip0 \
  && sudo -u pupcup test -r /dev/spidev0.0 \
  && sudo -u pupcup test -r /dev/spidev1.0 \
  && echo "device nodes OK (LCD build)"
#   OLED build: swap /dev/spidev1.0 for /dev/i2c-1
```

On the **device**: the boot splash ("PupCup", ~1.5 s) should give way to the
dog-selector scene on the display, with the LED bar idle (off). From a phone or
laptop on the same wifi, open `http://pupcup.local/` (or
`http://192.168.0.141/`) — the dashboard loads with the seeded dogs.

### Running in the foreground for debugging

If the service won't come up and the journal isn't enough, stop it and run the
binary directly. Binding `:80` needs `CAP_NET_BIND_SERVICE` (the service has it,
a bare shell doesn't), so use a high port for a manual run:

```sh
sudo systemctl stop pupcup
sudo -u pupcup env PUPCUP_LISTEN=:8080 /opt/pupcup/pupcup --config /etc/pupcup/config.yaml
#   browse http://pupcup.local:8080/ ; Ctrl-C to quit
sudo systemctl start pupcup    # restore the real service when done
```

A config error makes the daemon **fail fast** with a clear `invalid config:`
message — this path surfaces it immediately.

---

## 7. Verification / UAT

Full acceptance is in [pupcup_build_plan.md §13](pupcup_build_plan.md). Run every
item on the **deployed device** and check them off. ("Display" below means the
GC9A01 LCD on the reference build, or the OLED if you built that variant.)

### 7.1 Install & service health
- [ ] `systemctl status pupcup` shows `active (running)`, no restart thrash.
- [ ] `journalctl -u pupcup` shows structured JSON logs ending in `device ready`.
- [ ] First boot logged `seeded dogs` once; a restart does **not** re-seed.
- [ ] `sudo -u pupcup` can read `/dev/gpiochip0`, `/dev/spidev0.0`, and
      `/dev/spidev1.0` (LCD) or `/dev/i2c-1` (OLED).
- [ ] `/opt/pupcup/pupcup` is `root:root 0755`; `/var/lib/pupcup` is `pupcup:pupcup`.

### 7.2 Cold-boot
- [ ] Power-cycle the Pi cold. Within ~30 s the display shows the dog selector.
- [ ] `pupcup.local` (and `192.168.0.141`) loads the dashboard from a phone.
- [ ] LED bar is off while idle.

### 7.3 Button-driven feeding (the device)
- [ ] Rotate the dial — display cycles through dogs; wraps at the ends.
- [ ] Select dog A, tap **GREEN** — display confirms; dashboard shows A fed **full**.
- [ ] Select dog B, tap **YELLOW** — **partial** recorded.
- [ ] Select dog C, tap **RED** — **none** recorded.
- [ ] After the last dog's meal the LED turns **solid green** and the 4 h lock is
      timed **from that last meal**.
- [ ] Display transitions to the locked-summary scene listing each dog's score.
- [ ] Grace timeout: feed only 2 of 3 dogs, wait `meal_complete_grace_minutes`
      (15) — device locks with the two; the third has no record (add later on web).
- [ ] Add-in chord: select a dog, **hold GREEN**, tap **BLUE** while holding —
      add-in picker appears ranked by that dog's history with an "Other (name
      later)" row last. Dial to scroll, short-press the rotary on a tag — meal
      commits with that tag, advances to the next dog.
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
      display returns to the selector.
- [ ] After 4 h the lock auto-clears on its own.

### 7.4 Web app
- [ ] Dashboard shows each dog's **last-fed time + score** accurately.
- [ ] Edit a past feeding (timestamp + score) — reflected in `/history` and the
      dog's detail chart (`/dogs/{id}`).
- [ ] Soft-delete a feeding — it leaves the table; the chart updates.
- [ ] Add a retroactive feeding with a custom timestamp — lands in the right spot.
- [ ] On `/feedings`, add two add-in tags to one meal via chips — both persist
      and show on the dog's history; remove one — it detaches, the other stays.
- [ ] Create a brand-new tag from the create-on-the-fly field — reusable on the
      next feeding and visible in `/tags`.
- [ ] Resolve a device "Other" feeding: open the flagged one, replace Unspecified
      with a real tag — the flag clears.
- [ ] Archive a tag in `/tags` — gone from new pickers, still shown on past
      feedings that used it.
- [ ] Per-dog tag ranking: a tag used often for dog A sorts above unused tags in
      A's picker (device + web), and is **not** mirrored to dog B.
- [ ] Add an illness event spanning yesterday→today with "ongoing"; later set the
      end date.
- [ ] Add a stress event for the whole household.
- [ ] Manage dogs: rename, change accent color, upload a photo (appears on the
      dashboard); an over-limit image (>150 KB or >320×320) is rejected clearly.

### 7.5 Resilience
- [ ] Reboot with home wifi off, press a button — the feeding is recorded (not
      lost) and flagged **"time unverified"** on the web (the clock hadn't NTP-
      synced). Re-enable wifi — `pupcup.local` reachable, the entry appears, and
      editing its time clears the flag.
- [ ] Re-run `./deploy/deploy.sh` while the device is **locked** — service
      restarts, display briefly blanks, LED bar resumes green, lock state is
      preserved (confirm via dashboard).
- [ ] Pull power abruptly mid-write — on next boot no DB corruption (SQLite WAL)
      and the state machine resumes. (A feeding pressed in the final instant
      before the cut may be lost — `synchronous=NORMAL`, the accepted stance —
      and is re-addable on the web.)
- [ ] `systemctl status pupcup` still `active (running)`; journald has the logs.

### 7.6 Polish
- [ ] Dashboard renders cleanly on iPhone Safari and a desktop browser.
- [ ] No perceptible input lag (< 100 ms button-to-display).
- [ ] No spurious feedings from button bounce after a vigorous press.
- [ ] Display text is legible from across the room.

**When every box is checked, v1 is shippable.**

---

## 8. Troubleshooting

| Symptom | First thing to check |
|---|---|
| Service won't start / restart-loops | `journalctl -u pupcup -e` — a config error fails fast with `invalid config:`. Run foreground (§6) to see it plainly. |
| `bind: permission denied` on `:80` | Manual foreground run lacks `CAP_NET_BIND_SERVICE` — use `PUPCUP_LISTEN=:8080`, or run via systemd (which has the cap). |
| `bind: address already in use` | Something else holds `:80`. `sudo ss -ltnp 'sport = :80'`. |
| Hardware errors at startup | `sudo -u pupcup test -r /dev/spidev1.0 …` (§6); re-check `groups pupcup` and that the buses are enabled (§2.3). |
| Display blank (LCD) | `/dev/spidev1.0` missing → `spi1-1cs` overlay didn't apply (§2.3); else check DC/RST wiring. |
| Display blank (OLED) | `sudo i2cdetect -y 1` must show `0x3C`; if absent it's wiring/kernel, not the app. |
| `pupcup.local` won't resolve | avahi up? (`systemctl status avahi-daemon`). Fall back to `192.168.0.141`. The app's own mDNS only advertises while it's running. |
| Wrong timestamps | `timedatectl` — is the clock NTP-synced? Confirm `timezone` in the config. There's no RTC; feedings made before sync are flagged "time unverified" on the web and editable there (§2.4). |
| DB write errors | `/var/lib/pupcup` must be `pupcup:pupcup` and writable; the unit's `StateDirectory` keeps it so. |

Useful one-liners (on the Pi):

```sh
sudo systemctl status pupcup --no-pager
journalctl -u pupcup -f                 # follow live
journalctl -u pupcup -b                 # this boot only
sudo systemctl restart pupcup
curl -fsS http://localhost/healthz && echo OK   # liveness probe
```

---

## 9. Sign-off

- [ ] §1–2 OS flashed and environment provisioned; §3 verification clean.
- [ ] §4 bootstrap done; §5 first deploy completed; service enabled and running.
- [ ] §6 first launch clean (READY, logs, device nodes, display, web).
- [ ] §7.1–7.6 UAT all checked.

Once signed off, record the result in
[pupcup_build_plan.md §13](pupcup_build_plan.md) (milestone 15) and v1 is ready
for household use.
