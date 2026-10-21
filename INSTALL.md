# Installing the companion

> **Draft for v0.1.** The player version isn't released yet. This is what it will be. Until then,
> upload with `/gf export` in the game and paste the text at
> <https://gold-forever.evensberget.com/me/upload>.

You download one file, double-click it, and approve the link in your browser. That's all. It
installs itself for your user only. It never asks for admin rights.

## Windows

1. Download `GoldForever-Companion.exe` from the
   [latest release](https://github.com/magnusev/gold-forever-companion/releases/latest).
2. Double-click it. Windows shows a blue box: **"Windows protected your PC"**.

   ![SmartScreen's first box](docs/img/smartscreen.png)

3. Click **More info**, then **Run anyway**.

   ![SmartScreen after More info](docs/img/smartscreen-run-anyway.png)

**Why Windows warns:** the file isn't signed with a paid Microsoft certificate. Windows warns about
any program it hasn't seen many times. It doesn't mean the file is harmful. If you want to be sure,
[VERIFY.md](VERIFY.md) shows how to check that the file was built from this source.

**Antivirus:** a program that copies itself and starts with Windows is also something malware does,
so an antivirus may flag it. The companion installs for your user only, and its first page says
exactly what it did and where.

## macOS

1. Download `GoldForever-Companion.dmg` from the
   [latest release](https://github.com/magnusev/gold-forever-companion/releases/latest).
2. Open it and drag **Gold Forever Companion** to **Applications**.
3. Open it from Applications. macOS says it **cannot be opened because Apple cannot check it**.
   Click **Done**.

   ![macOS refuses the first time](docs/img/macos-blocked.png)

4. Open **System Settings → Privacy & Security**. Scroll down to the message about Gold Forever
   Companion, and click **Open Anyway**. Confirm with your password or Touch ID.

   ![Privacy & Security, Open Anyway](docs/img/macos-open-anyway.png)

5. Open it again. This time it starts, and a small icon appears in the menu bar.

**Why macOS warns:** the app isn't notarized by Apple, which needs a paid Apple developer account.
Newer macOS versions no longer offer right-click → Open for this, so step 4 is the way.

## Linux

1. Download `gold-forever-companion-linux-amd64` (or `-arm64`) from the
   [latest release](https://github.com/magnusev/gold-forever-companion/releases/latest).
2. Make it runnable and run it once:

   ```sh
   chmod +x gold-forever-companion-linux-amd64
   ./gold-forever-companion-linux-amd64
   ```

There's no warning on Linux.

## The first run, on every system

The companion opens a page in your browser. That page is served by the companion itself, on your
own computer.

![The first-run page](docs/img/first-run.png)

1. **It says what it installed, and where** (see the table below).
2. **"Start with my computer"** is ticked. Leave it ticked, so new saves upload without you
   remembering to start it. Untick it if you'd rather start it yourself.
3. **Link your account:** the page shows a code and opens <https://gold-forever.evensberget.com/link>.
   Sign in with Battle.net, check that the code matches, and click **Approve**.
4. **It finds World of Warcraft** and lists every account folder it found. Each has a switch. If your
   game is somewhere unusual, use **Add folder**.

From then on, every time the game saves (logout, `/reload`), the companion uploads the new save.
The icon in the tray or menu bar shows when it last uploaded.

## What it puts on your computer

| | Windows | macOS | Linux |
|---|---|---|---|
| The program | `%LOCALAPPDATA%\Gold Forever\` | `/Applications/Gold Forever Companion.app` (you put it there) | `~/.local/bin/` |
| Start menu / launcher | a Start-menu shortcut | — | `~/.local/share/applications/` |
| Start with my computer | the Startup folder | a LaunchAgent in `~/Library/LaunchAgents/` | `~/.config/autostart/` |
| Settings and token | `%APPDATA%\gold-forever-companion\config.json` | `~/Library/Application Support/gold-forever-companion/config.json` | `~/.config/gold-forever-companion/config.json` |
| Backups and log | `%LOCALAPPDATA%\gold-forever-companion\` | `~/Library/Application Support/gold-forever-companion/` | `~/.local/share/gold-forever-companion/` |

Nothing else is written. The only folder it writes to in your game's folder is
`Interface/AddOns/GoldForever_Data/`, and only once prices into the game exist (a later version).

## Uninstall

Open the settings page (from the tray or menu bar icon) and click **Uninstall**. It removes the
program, the shortcut and "start with my computer". It asks whether to keep your backups and log.

On macOS, also drag the app from Applications to the Bin.

Your account on the site isn't touched. To stop uploads without uninstalling, revoke the
companion's token at <https://gold-forever.evensberget.com/me/tokens>.

---

## Screenshots still needed (TODO)

Taken from real machines, with a real v0.1 release:

- [ ] `docs/img/smartscreen.png`: Windows 11, the first SmartScreen box, "Windows protected your PC"
- [ ] `docs/img/smartscreen-run-anyway.png`: the same box after **More info**, showing **Run anyway**
- [ ] `docs/img/macos-blocked.png`: macOS 15 or newer, "cannot be opened because Apple cannot check it"
- [ ] `docs/img/macos-open-anyway.png`: System Settings → Privacy & Security, with **Open Anyway**
- [ ] `docs/img/first-run.png`: the companion's first-run page (any system)
