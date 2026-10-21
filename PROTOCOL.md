# Protocol

Every request the companion makes, all to one site (by default `https://gold-forever.app`).

The site moved there from `gold-forever.evensberget.com` on 2026-10-22; the old name answers 308 to the
same path on the new one. A redirect normally loses the `Authorization` header when it changes domain,
so the companion carries the token across a redirect only to `https://gold-forever.app`, and to no other
host (`internal/api`, `carryToken`). Another client that follows the 308 should do the same, or use
the new address directly.
Anything here can be done with `curl`, so you can check the app against this list or write your own
client. A request not listed here is a bug.

Requests marked **planned** don't exist yet. They are listed so the whole surface is known before it's
built (design 006 in the Gold Forever repository).

## Your token

Make one at `/me/tokens`, or link the app (planned, below). Send it as `Authorization: Bearer <token>`.
It's shown once, and the server keeps only its hash. Revoking it at `/me/tokens` stops every client
that uses it.

## Upload a save

```http
POST /api/v1/saves?name=GoldForever.lua
Authorization: Bearer gf_...
Content-Type: application/octet-stream
User-Agent: gold-forever-companion/<version> (<os>)

<the bytes of GoldForever.lua, exactly as the game wrote them>
```

```sh
curl -H "Authorization: Bearer $GF_TOKEN" -H 'Content-Type: application/octet-stream' \
  --data-binary @GoldForever.lua 'https://gold-forever.app/api/v1/saves?name=GoldForever.lua'
```

- **What the server reads:** the file itself. `name` only labels the upload in your list. Nothing
  about your disk is sent: no path, no WoW account folder name, no computer name.
- **Limits:** 16 MB per file. Sending the same file twice is harmless, because the server recognises
  it by its content hash and answers `already imported`.

| Answer | Body | What a client does |
|---|---|---|
| 200 | `{"status":"imported","problems":0,"saves":12}` | Done |
| 200 | `{"status":"already imported","saves":12}` | Done: the server had it |
| 400 | `{"status":"unknown game","reason":"unknown game version 30403"}` | Shows the reason; doesn't resend |
| 401 | `{"status":"unauthorized"}` | Stops; the token is wrong or revoked |
| 403 | `{"status":"…"}` (account suspended, or a token without upload rights) | Stops |
| 413 | `{"status":"too large"}` | Shows it; doesn't resend |
| 415 | `{"status":"send the file as application/octet-stream"}` | A client bug |
| 422 | `{"status":"rejected","reason":"…"}` (not readable as a save) | Waits for the next save |
| 429 | `Retry-After: <seconds>` (planned) | Waits that long, then tries again |
| 5xx, or no answer | — | Tries again later, waiting longer each time |

The game decides which game a save belongs to: WoW: Forever or retail, read from the save's own
`interface` number. A save from any other game is refused with 400.

## The latest version

```http
GET /api/v1/companion/latest
```

`{"version":"v0.1.0","url":"https://github.com/magnusev/gold-forever-companion/releases/tag/v0.1.0"}`

Asked at start and then once a day, with no token, so it says nothing about who asks beyond the
`User-Agent`. A 404 means the server names no release. When the answer is newer, the settings page
shows a notice with the link: the app never downloads or installs anything itself.

```sh
curl https://gold-forever.app/api/v1/companion/latest
```

## Linking the app (a device code, as in RFC 8628)

`gold-forever-companion link` does this, and so will the settings page. No token is sent: linking is
how the app gets one.

1. **The app asks for a code:**

   ```http
   POST /api/v1/device/code
   Content-Type: application/json

   {"client":"gold-forever-companion","hostname":"<this computer's name>"}
   ```

   `{"device_code":"…","user_code":"BCDF-GHJK","verification_uri":"https://…/link","verification_uri_complete":"https://…/link?code=BCDF-GHJK","expires_in":600,"interval":5}`

   - The hostname only names the token you approve ("Companion on <hostname>"), so you can tell your
     devices apart at `/me/tokens`. The server keeps letters, digits, spaces and `-_.`, at most 50;
     a computer with no usable name is sent as "this computer".
   - The `device_code` is the app's secret for the next step and is never shown. The `user_code` is
     what you check on the site.
   - 503 means too many codes are waiting on the server: try again later.

2. **You approve it:** you open the link, sign in, check that the code matches the one the app
   shows, and approve or deny.

3. **The app waits for the token,** asking every `interval` seconds (5):

   ```http
   POST /api/v1/device/token
   Content-Type: application/json

   {"device_code":"…"}
   ```

   - 400 `{"error":"authorization_pending"}`: not decided yet; ask again.
   - 400 `{"error":"slow_down"}`: asked too soon. The app adds 5 seconds to its interval for good.
   - 400 `{"error":"access_denied"}`: you denied it. The app stops.
   - 400 `{"error":"expired_token"}`: the code is past its 10 minutes, unknown, or already used. The
     app stops; it also stops on its own once `expires_in` has passed.
   - 200 `{"access_token":"gf_…","token_type":"Bearer","scope":"saves:write prices:read"}`: the token,
     given once. The app saves it in its settings file (readable only by you) and never prints it.
   - A 5xx answer or no answer is waited out until the code expires.

## Planned: prices into the game

```http
GET /api/v1/prices?game=forever&market=forever-EU-normal-horde
Authorization: Bearer gf_...
```

The prices arrive as JSON: item ids and copper values. The app checks every number and writes
`Interface/AddOns/GoldForever_Data/Data.lua` itself. The server never sends Lua, so a compromised
server could send wrong prices but could never run code in your game.
