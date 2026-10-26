# Gold Forever companion

A small, open-source program that sends the `GoldForever.lua` file World of Warcraft writes to
[Gold Forever](https://gold-forever.app), so your characters, scans and ledger show up there
without copying anything by hand. It also brings the shared market prices back into the game, so
the Gold Forever addon can show them in item tooltips.

## Getting it

Download one file from the [latest release](https://github.com/gold-forever/companion/releases/latest),
double-click it, and approve the link in your browser. It installs itself for your user only (no
admin rights), starts with your computer, finds World of Warcraft, and uploads every new save on its
own. Uninstall is a button in its settings. [INSTALL.md](INSTALL.md) walks through it, warnings
included.

If you'd rather not run it, you can still upload in the game with `/gf export` and paste the text at
<https://gold-forever.app/me/upload>.

## Prices in the game

Every hour, and right after each of your uploads, the companion fetches the prices of your
characters' markets (each realm and faction you play, Forever and retail) and writes them into a
small addon, `GoldForever_Data`, in your game folder. The Gold Forever addon (0.15.0 or newer) shows
them in item tooltips.

- **The first time, restart the game fully** (quit and start it again). WoW only sees a new addon
  folder at start, so `/reload` is not enough the first time.
- **After that, `/reload` or logging in** picks up the newest prices.
- **To see what was written,** open the settings page: the **Prices in the game** table shows each
  game folder, its markets, how many items, and the last fetch.

The prices come from everyone's uploads: every upload adds its auction scans, item details and loot
counts to the shared prices, never your name. Your characters, gold and ledger stay private.

## What it sends, and what it doesn't

- **Sent:** the bytes of `GoldForever.lua`, exactly as the game wrote them, with your token. The
  `User-Agent` says the companion's version and your operating system.
- **Asked once a day, without your token:** whether a newer companion exists, so the settings page
  can say so. It never downloads one itself.
- **Never sent:** the file's path, your WoW account folder's name, your computer's name, or anything
  else on your disk.

Every request it can make is listed in [PROTOCOL.md](PROTOCOL.md). If you'd rather not run this
program at all, the same uploads are a few lines of shell: see [scripts/](scripts/).

## Without the app: the scripts

`scripts/gf-upload.sh` (Linux, macOS) and `scripts/gf-upload.ps1` (Windows) do the upload part in about
50 lines each. They watch every `GoldForever.lua` under your WoW folder and send each new save.

```sh
GF_TOKEN=gf_... scripts/gf-upload.sh "$HOME/Games/battlenet/drive_c/Program Files (x86)/World of Warcraft"
```

```powershell
$env:GF_TOKEN = "gf_..."
Unblock-File .\gf-upload.ps1     # Windows blocks scripts downloaded from the internet until you do
.\gf-upload.ps1 -Wow "C:\Program Files (x86)\World of Warcraft"
```

If PowerShell still refuses ("running scripts is disabled on this system"), run it once with
`powershell -ExecutionPolicy Bypass -File .\gf-upload.ps1 -Wow "…"`. That changes nothing permanently.

## Checking that a download matches this source

Releases are not signed with a Windows or Apple certificate, so your system will warn before running
one. Instead, every file can be checked: `gh attestation verify` shows that this repository's release
workflow built it, and `make reproduce` rebuilds it from the tag with identical bytes (Windows and
Linux). [VERIFY.md](VERIFY.md) has the steps, and how to read the companion's own log of what it sent.

## For developers

Started with no arguments, as a double-click does, the companion is the player's app:

- **From anywhere but its install folder,** it installs itself (Windows: `%LOCALAPPDATA%\Gold Forever`
  with a Start-menu shortcut and the current user's Run key; Linux: `~/.local/bin` with a `.desktop`
  launcher and an XDG autostart entry; macOS: the `.app` stays where you put it and gains a LaunchAgent),
  starts the installed copy and exits. A download newer than the installed copy replaces it; one no
  newer just shows the running companion.
- **Installed,** it runs once per user: the tray icon, the uploads, and a settings page on
  `127.0.0.1` at a random port, which only takes requests for that exact address, from no other site,
  with a random secret. Autostart starts it with `--autostart`, which stays quiet in the tray.
- **Uninstall,** on the settings page, removes the binary, the shortcut, autostart and the settings
  with the token, and the backups and log unless you keep them.

The Windows build is a windowed program, so it opens no console window and prints nothing: use the
Linux or macOS build for the command line below.

`link` connects this computer to your account, `run` watches every World of Warcraft install it finds
and uploads each new save without the tray or the page, and `--once --file` uploads one file and exits.

```sh
gold-forever-companion link   # prints a code and a link; approve it on the site, and the token is saved
gold-forever-companion run
gold-forever-companion --once --file ".../WTF/Account/<ACCOUNT>/SavedVariables/GoldForever.lua"
```

Instead of `link`, you can make a token at /me/tokens and set `GF_TOKEN`, which keeps it out of your
shell history (PowerShell: `$env:GF_TOKEN = "gf_..."`). `run` reads its
settings from `config.json` in your system's config folder (`~/.config/gold-forever-companion/` on
Linux): `token`, `url`, `folders` (WoW folders it doesn't find by itself) and `off` (saves to skip, by
path). The file is readable by you only. Like the app, `run` is one copy per user: while the app or
another `run` is running, it says so and exits with an error.

What `run` does, in order:

1. **Finds the installs:** Battle.net's registry entry and `Program Files (x86)` on Windows,
   `/Applications` on macOS, the usual Wine, Lutris, Bottles and Steam prefixes on Linux, plus your
   `folders`. In each, every `_flavour_` folder's `WTF/Account/*/SavedVariables/GoldForever.lua`.
   Two accounts played at once on one install, from one Battle.net login or two, are supported: each save is uploaded on its own.
2. **Waits for the game to finish writing:** a save counts once it has been unchanged for 2 seconds
   and ends with the bare `}` that closes `GoldForeverDB`. A cut-off file waits for the next change.
3. **Keeps a copy** in your data folder (`~/.local/share/gold-forever-companion/backups` on Linux), the
   last 10 per save. A copy is pending until the server has answered for it, so nothing is lost if
   the computer shuts down first.
4. **Sends pending copies, oldest first.** A server that is down, or any answer that is not the
   server's own, is tried again after 1 minute, then 2, 4 … up to 30, and never sooner than a
   `Retry-After` it sent. A 429 waits as long as it says. An account that may not upload (403) is
   asked again every 30 minutes. A refused token (401) stops it until you link again. A save refused
   as too large (413) is sent again at the next start and once a day, since the server's limit may be
   raised, until it goes through or a newer save replaces it.
5. **Logs every answer** in `sent.jsonl` next to the backups: time, save, size, SHA-256 and the
   answer, kept 30 days.

To build:

```sh
make test
make build          # dist/: Windows and Linux, amd64 and arm64 (macOS needs a Mac: the tray uses cgo there)
```

## Licence

[MIT](LICENSE).
