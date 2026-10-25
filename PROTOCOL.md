# Protocol

Every request the companion makes, all to one site (by default `https://gold-forever.app`).

The site moved there from `gold-forever.evensberget.com` on 2026-10-22; the old name answers 308 to the
same path on the new one. A redirect normally loses the `Authorization` header when it changes domain,
so the companion carries the token across a redirect only to `https://gold-forever.app`, and to no other
host (`internal/api`, `carryToken`). Another client that follows the 308 should do the same, or use
the new address directly.
Anything here can be done with `curl`, so you can check the app against this list or write your own
client. A request not listed here is a bug.

Requests marked **planned** are not made by the companion yet. They are listed so the whole surface is
known before it's built (design 006 in the Gold Forever repository).

## Your token

Make one at `/me/tokens`, or link the app (below). Send it as `Authorization: Bearer <token>`.
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
- **Limits:** 10 MiB for a WoW: Forever save and 32 MiB for a retail one, told apart by the save's own
  `interface` number. Sending the same file twice is harmless, because the server recognises it by its
  content hash and answers `already imported`.

| Answer | Body | What a client does |
|---|---|---|
| 200 | `{"status":"imported","problems":0,"saves":12}` | Done |
| 200 | `{"status":"already imported","saves":12}` | Done: the server had it |
| 200 | anything else (a proxy's or a captive portal's page) | Not the server's answer: tries again later |
| 400 | `{"status":"unknown game","reason":"unknown game version 30403"}` | Shows the reason; doesn't resend |
| 401 | `{"status":"unauthorized"}` | Stops; the token is wrong or revoked |
| 403 | `{"status":"…"}` (account suspended, or a token without upload rights) | Keeps the save; tries again in 30 minutes |
| 413 | `{"status":"too large","reason":"a forever save may be at most 10 MB"}` (no `reason` past the 32 MiB transport limit) | Shows it, with the reason; doesn't resend |
| 415 | `{"status":"send the file as application/octet-stream"}` | A client bug; doesn't resend |
| 422 | `{"status":"rejected","reason":"…"}` (not readable as a save) | Waits for the next save |
| 429 | `Retry-After: <seconds>`, `{"status":"rate limited","reason":"too many uploads with this token; try again in 40 s"}` | Waits that long, then tries again |
| 429 | `Retry-After: 10`–`30`, `{"status":"busy","reason":"too many uploads at once; try again shortly"}` (the import queue is full) | Waits that long, then tries again |
| 503 | `Retry-After: 600`, `{"status":"paused","reason":"<the operator's message>"}` (uploads are paused) | Shows the reason; waits at least that long, then tries again |
| other 5xx, or no answer | — | Tries again later, waiting longer each time (1 minute, doubling to 30), and at least `Retry-After` when sent |
| any other status (404, 408, …) | — | Not the server's answer about the save (a proxy mid-deploy, a wrong `url`): tries again later, waiting longer each time |

The game decides which game a save belongs to: WoW: Forever or retail, read from the save's own
`interface` number. A save from any other game is refused with 400.

## The latest version

```http
GET /api/v1/companion/latest
```

`{"version":"v0.1.0","url":"https://github.com/gold-forever/companion/releases/tag/v0.1.0"}`

Asked at start and then once a day, with no token, so it says nothing about who asks beyond the
`User-Agent`. A 404 means the server names no release. When the answer is newer, the settings page
shows a notice with the link: the app never downloads or installs anything itself. A `url` that is not
under `https://github.com/gold-forever/companion/releases/` or on the site is refused, with no notice.

```sh
curl https://gold-forever.app/api/v1/companion/latest
```

## Linking the app (a device code, as in RFC 8628)

`gold-forever-companion link` does this, and so does "Link this computer" on the settings page. No token is sent: linking is
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
   - The app opens `verification_uri_complete` in your browser only when it is a page of the site it
     talks to (or of `https://gold-forever.app`); any other link is refused, never opened.
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

The server answers this today; the companion does not ask it yet.

```http
GET /api/v1/prices?game=forever
GET /api/v1/prices?game=forever&market=forever-EU-normal-horde
Authorization: Bearer gf_...
If-None-Match: "<the ETag of the last answer>"
```

- `game` is `forever` (the default) or `retail`; an unknown one is 400.
- Without `market`, the answer holds every market of your own characters in that game that has pooled
  prices (at most 8): `{"v":…,"game":"forever","at":<epoch s>,"markets":[{"market":"…","items":[…]}]}`.
  With one, that market only: `{"v":…,"game":"forever","market":"…","at":…,"items":[…]}`, or 404 when
  the server prices no such market.
- Each item is `{"id":<item id>,"value":<copper>,"minBuyout":<copper>,"players":<n>,"scannedAt":<epoch s>,"week":<copper>}`;
  `week` is left out when there are too few days to tell.
- The answer carries an `ETag`; sending it back as `If-None-Match` gets 304 with no body while nothing
  changed.
- 401 for a wrong or revoked token, 403 for a suspended account or a token without `prices:read`, and
  503 with `Retry-After: 15` while the server is still loading its prices after a start.

The app checks every number and writes `Interface/AddOns/GoldForever_Data/Data.lua` itself. The server
never sends Lua, so a compromised server could send wrong prices but could never run code in your game.
