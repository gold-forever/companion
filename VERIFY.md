# Checking a release

The companion is not signed with a Windows or Apple certificate. These checks replace a signature:
they show that a file came from this repository, and that this source makes exactly that file. You
can run any of them. None needs an account with us.

## 1. Who built it: the attestation

Every file in a release has a build attestation. GitHub records which workflow built the file, from
which commit, and its SHA-256. To check a file you downloaded, install the [GitHub CLI](https://cli.github.com)
and run:

```sh
gh attestation verify GoldForever-Companion.exe --repo gold-forever/companion
```

Use the name of the file you downloaded. It passes only when GitHub built that exact file from this
repository's release workflow (`.github/workflows/release.yml`). The output names the commit and the
tag. If you changed even one byte, it fails.

**Releases up to v0.1.0-rc.6** were built before the repository moved from `magnusev/gold-forever-companion`
to `gold-forever/companion` (2026-10-22), and their attestations name the old repository. Verify those with:

```sh
gh attestation verify GoldForever-Companion.exe --owner magnusev --signer-repo magnusev/gold-forever-companion
```

`--signer-repo` pins the exact repository that built it; the same check with any other repository fails.

## 2. That the source makes these bytes: rebuild it

You can build the release yourself and compare. The Windows and Linux files come out identical, byte
for byte.

1. Get the source at the tag, and the release's `SHA256SUMS`:

   ```sh
   git clone https://github.com/gold-forever/companion
   cd companion
   git checkout v0.1.0
   gh release download v0.1.0 --pattern SHA256SUMS
   ```

2. Rebuild and compare:

   ```sh
   make reproduce TAG=v0.1.0
   ```

   It builds the four Windows and Linux files (`GoldForever-Companion.exe`,
   `GoldForever-Companion-arm64.exe`, `gold-forever-companion-linux-amd64` and `-arm64`) into `reproduce/`, writes their hashes to
   `reproduce/SHA256SUMS.local`, and checks them against the release's `SHA256SUMS`. Each line says
   `OK`. A mismatch says `FAILED`.

**Go's version:** `go.mod` names the exact Go toolchain (`toolchain go1.27.3`), and `go` downloads it
when yours differs. If you don't want Go installed, use the official image:

```sh
podman run --rm -v "$PWD":/src:z -w /src docker.io/library/golang:1.27.3 make reproduce TAG=v0.1.0
```

(`docker run` works the same way.)

**`TAG` must be the tag exactly, `v` included.** The version is built into the binary, so
`TAG=0.1.0` gives different bytes.

### macOS: best effort

`GoldForever-Companion.dmg` holds `Gold Forever Companion.app`, and the program inside it is one
universal binary built on GitHub's Mac runner with cgo. The release also publishes that binary on its
own, as `gold-forever-companion-macos-universal`, so you can check the one you run:

```sh
cmp "/Applications/Gold Forever Companion.app/Contents/MacOS/gold-forever-companion" \
    gold-forever-companion-macos-universal && echo "the same bytes"
```

Then check that binary's attestation (section 1). The `.dmg` itself is never byte for byte
reproducible, because macOS writes times into it.

To rebuild the binary, its bytes also depend on the Xcode version. The release log prints it: open the
release's workflow run, job `macos`, step `xcodebuild -version && go version`. With the same Xcode,
on a Mac, at the tag:

```sh
make macos VERSION=v0.1.0
shasum -a 256 dist/gold-forever-companion-macos-universal
```

Compare the hash with the `gold-forever-companion-macos-universal` line in `SHA256SUMS`. With a
different Xcode it may differ, even though the source is the same. For a Mac, the attestation is the
dependable check.

## 3. What it sent: the local log

The companion writes one line for every answer the server gives, on your own computer, and keeps
30 days. The file is `sent.jsonl` in its data folder:

| System | Data folder |
|---|---|
| Windows | `%LOCALAPPDATA%\gold-forever-companion\` |
| macOS | `~/Library/Application Support/gold-forever-companion/` |
| Linux | `~/.local/share/gold-forever-companion/` |

Each line has the time, the save, its size in bytes, its SHA-256, and the server's answer. The copies
it sent are in `backups/` in the same folder, the last 10 per save. To check that a copy is what was
sent, hash it (`sha256sum`, or `Get-FileHash` in PowerShell) and look for that hash in `sent.jsonl`.

## 4. What it can send at all

[PROTOCOL.md](PROTOCOL.md) lists every request the companion makes. All of them go to one site,
`gold-forever.app` unless you change it. The code that sends is one function,
`Upload` in `internal/server/server.go`. To watch the traffic yourself, set `url` in `config.json` to a
server of your own and look at what arrives. Or read the [scripts](scripts/), which do the same
uploads in about 50 lines.
