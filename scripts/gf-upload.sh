#!/usr/bin/env bash
# Uploads every GoldForever.lua under a World of Warcraft folder to Gold Forever each time the game
# writes it (a /reload or a logout). The whole protocol, for anyone who would rather not run the app.
#
#   GF_TOKEN=gf_... scripts/gf-upload.sh "<World of Warcraft folder>" [site URL]
#
# Needs bash (3.2 is enough, as macOS ships), curl, and sha256sum or shasum. Stop it with Ctrl-C.
set -euo pipefail

wow=${1:?usage: GF_TOKEN=gf_... $0 "<World of Warcraft folder>" [site URL]}
url=${2:-https://gold-forever.app}
: "${GF_TOKEN:?set GF_TOKEN to a token from /me/tokens}"

state=$(mktemp -d)
trap 'rm -r -f -- "$state"' EXIT

# Size and modification time: GNU stat, else BSD (macOS).
stamp() { stat -c '%s %Y' "$1" 2>/dev/null || stat -f '%z %m' "$1"; }
# The game writes the file in more than one go: a complete one ends with the closing brace.
complete() { [[ $(tail -c 64 "$1" | tr -d '[:space:]') == *"}" ]]; }
key() { printf '%s' "$1" | cksum | cut -d ' ' -f 1; }

upload() {
  # The token goes in through a file descriptor, so it never shows in the process list.
  curl -sS -m 120 -H @<(printf 'Authorization: Bearer %s\n' "$GF_TOKEN") \
    -H 'Content-Type: application/octet-stream' -A 'gf-upload.sh' \
    --data-binary "@$1" -w ' (HTTP %{http_code})\n' "$url/api/v1/saves?name=GoldForever.lua" || true
}

shopt -s nullglob
echo "watching $wow every 2 s; Ctrl-C stops"
while true; do
  for file in "$wow"/_*_/WTF/Account/*/SavedVariables/GoldForever.lua; do
    k=$(key "$file")
    now=$(stamp "$file") || continue
    if [[ $now != "$(cat "$state/$k.seen" 2>/dev/null)" ]]; then
      # Changed since the last look: wait until it has stood still for 2 s.
      echo "$now" > "$state/$k.seen"
      echo "$SECONDS" > "$state/$k.at"
      continue
    fi
    (( SECONDS - $(cat "$state/$k.at") >= 2 )) || continue
    [[ $now != "$(cat "$state/$k.sent" 2>/dev/null)" ]] || continue
    complete "$file" || continue
    echo "$now" > "$state/$k.sent"
    # Only the local log names the folder; the upload carries the file's bytes and nothing else.
    echo "$(date +%H:%M:%S) ${file#"$wow"/}: $(upload "$file")"
  done
  sleep 2
done
