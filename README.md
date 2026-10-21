# Gold Forever companion

A small, open-source program that sends the `GoldForever.lua` file World of Warcraft writes to
[Gold Forever](https://gold-forever.evensberget.com), so your characters, scans and ledger show up there
without copying anything by hand. Later versions will also write market prices back into the game.

> **Status: early.** This version uploads one file when you run it. Watching your installs, the tray
> icon and the settings page come next.

## What it sends, and what it doesn't

- **Sent:** the bytes of `GoldForever.lua`, exactly as the game wrote them, with your token. The
  `User-Agent` says the companion's version and your operating system.
- **Never sent:** the file's path, your WoW account folder's name, your computer's name, or anything
  else on your disk.

Every request it can make is listed in [PROTOCOL.md](PROTOCOL.md). If you'd rather not run this
program at all, the same uploads are a few lines of shell: see [scripts/](scripts/).

## Using it

1. Make a token at <https://gold-forever.evensberget.com/me/tokens>.
2. Run it:

   ```sh
   export GF_TOKEN=gf_...        # PowerShell: $env:GF_TOKEN = "gf_..."
   gold-forever-companion --once --file "<WoW folder>/_retail_/WTF/Account/<ACCOUNT>/SavedVariables/GoldForever.lua"
   ```

   Using `GF_TOKEN` rather than `--token` keeps the token out of your shell history.

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

## Building

```sh
make test
make build          # dist/: Windows and Linux, amd64 and arm64
```

## Licence

[MIT](LICENSE).
