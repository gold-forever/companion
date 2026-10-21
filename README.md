# Gold Forever companion

A small, open-source program that sends the `GoldForever.lua` file World of Warcraft writes to
[Gold Forever](https://gold-forever.evensberget.com), so your characters, scans and ledger show up there
without copying anything by hand. Later versions will also write market prices back into the game.

## Getting it

**Not ready for players yet.** The first version for players (v0.1) will be one file: download it,
double-click it, and approve the link in your browser. It installs itself for your user only (no
admin rights), starts with your computer, finds World of Warcraft, and uploads every new save on its
own. Uninstall is a button in its settings.

Until then, upload in the game with `/gf export` and paste the text at
<https://gold-forever.evensberget.com/me/upload>.

The releases published so far (v0.0.x) are for developers: they upload one file from the command line
(see "For developers" below).

## What it sends, and what it doesn't

- **Sent:** the bytes of `GoldForever.lua`, exactly as the game wrote them, with your token. The
  `User-Agent` says the companion's version and your operating system.
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
one. Instead, each release can be checked two ways:

- **Who built it:** `gh attestation verify <file> --repo magnusev/gold-forever-companion` proves the
  file was built by this repository's release workflow from the tagged commit.
- **That the source makes exactly these bytes:** check out the tag, download its `SHA256SUMS`, and run
  `make reproduce TAG=<tag>`. The Windows and Linux binaries match byte for byte. Go's version is
  pinned in `go.mod`, so `go` fetches the same toolchain. The macOS binary depends on the Xcode
  version, which the release log prints.

## For developers

`run` watches every World of Warcraft install it finds and uploads each new save; `--once --file`
uploads one file and exits.

```sh
export GF_TOKEN=gf_...        # a token from /me/tokens; PowerShell: $env:GF_TOKEN = "gf_..."
gold-forever-companion run
gold-forever-companion --once --file ".../WTF/Account/<ACCOUNT>/SavedVariables/GoldForever.lua"
```

`GF_TOKEN` rather than `--token` keeps the token out of your shell history. `run` also reads its
settings from `config.json` in your system's config folder (`~/.config/gold-forever-companion/` on
Linux): `token`, `url`, `folders` (WoW folders it doesn't find by itself) and `off` (saves to skip, by
path). The file is readable by you only.

What `run` does, in order:

1. **Finds the installs:** Battle.net's registry entry and `Program Files (x86)` on Windows,
   `/Applications` on macOS, the usual Wine, Lutris, Bottles and Steam prefixes on Linux, plus your
   `folders`. In each, every `_flavour_` folder's `WTF/Account/*/SavedVariables/GoldForever.lua`.
2. **Waits for the game to finish writing:** a save counts once it has been unchanged for 2 seconds
   and ends with the bare `}` that closes `GoldForeverDB`. A cut-off file waits for the next change.
3. **Keeps a copy** in your data folder (`~/.local/share/gold-forever-companion/backups` on Linux), the
   last 10 per save. A copy is pending until the server has answered for it, so nothing is lost if
   the computer shuts down first.
4. **Sends pending copies, oldest first.** A server that is down is tried again after 1 minute, then
   2, 4 … up to 30. A 429 waits as long as it says. A refused token or account (401, 403) stops it.
5. **Logs every answer** in `sent.jsonl` next to the backups: time, save, size, SHA-256 and the
   answer, kept 30 days.

To build:

```sh
make test
make build          # dist/: Windows and Linux, amd64 and arm64
```

## Licence

[MIT](LICENSE).
