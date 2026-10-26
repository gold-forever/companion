# Protocol

Every request the companion makes, all to one site (by default `https://gold-forever.app`).

The site moved there from `gold-forever.evensberget.com` on 2026-10-22; the old name answers 308 to the
same path on the new one. A redirect normally loses the `Authorization` header when it changes domain,
so the companion carries the token across a redirect only to `https://gold-forever.app`, and to no other
host (`internal/server`, `carryToken`). Another client that follows the 308 should do the same, or use
the new address directly.
Anything here can be done with `curl`, so you can check the app against this list or write your own
client. A request not listed here is a bug. The server's own statement of these routes is the OpenAPI
spec in [`internal/server/openapi.yaml`](internal/server/openapi.yaml), which the app's request and
answer types are generated from; where this page and the spec differ, the spec is right.

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
| 413 | `{"status":"too large","reason":"a forever save may be at most 10 MB"}` (no `reason` past the 32 MiB transport limit) | Shows it, with the reason; sends the same save again at the next start and once a day, until it goes through or a newer save replaces it |
| 415 | `{"status":"send the file as application/octet-stream"}` | A client bug; doesn't resend |
| 422 | `{"status":"rejected","reason":"…"}` (not readable as a save) | Waits for the next save |
| 429 | `Retry-After: <seconds>`, `{"status":"rate limited","reason":"too many uploads from this account; try again in 40 s"}` (the limit is per account, across all its tokens) | Waits that long, then tries again |
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

## Which account the token uploads to

```http
GET /api/v1/companion/account
Authorization: Bearer gf_...
```

`{"publicName":null,"battleTag":"Goldsmith#1234"}`

Asked at start and after each link, so the settings page and the tray can say "Uploading to
Goldsmith#1234", or the public name once one is set. The answer stays in the app's memory: it is shown
only to you, in the app, and never logged or sent anywhere. A 404 (a server from before the route), a
401 or 403, or no connection shows no account and changes nothing else; a failed ask keeps the account
already shown, and while the server cannot be reached it is asked again later.

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

## Prices into the game

At start, every hour, and right after one of its own uploads answers `imported`, one request per
game a save was found for (Forever, interface 16000–16999: `game=forever`; retail, interface 110000
and up: `game=retail`) for the markets of your account's characters in that game:

```http
GET /api/v1/prices?game=forever
Authorization: Bearer gf_...
If-None-Match: "<the ETag of the prices in the game>"
```

`game` is `forever` (the server's default) or `retail`; an unknown one is 400. Without `market`, the
answer holds every market of your own characters in that game that has pooled prices (at most 8). A
market in the settings (`"market"`) or `run --market` asks for that one market instead in the game
its key starts with (`&market=forever-EU-normal-horde`), to try a market out; the server answers 404
when it prices no such market.

`If-None-Match` is sent only when every flavour of the game already has the `Data.lua` of that same
request; what was written and its one ETag are kept per game in `prices.json` in the app's data folder.
A failing game leaves the other's prices written.

- 200 `{"v":1,"game":"forever","at":<epoch s>,"markets":[{"market":"forever-EU-normal-horde","items":[{"id":2589,"value":1234,"minBuyout":1100,"players":1,"scannedAt":<epoch s>}, …]}, …]}`
  (for one market: `{"v":1,"game":"forever","market":"…","at":…,"items":[…]}`): prices in integer
  copper. The app checks every field (version 1, the game and market it asked for, plain integers,
  ids above 0, nothing negative or past 2^53, market keys of letters, digits, `-` and `_`, none twice);
  any fault refuses the whole answer and the last good file stays. No markets writes an empty table.
  An item may also carry `"week"`, the median of its daily pooled values over the 7 days before its
  `scannedAt`, in copper, absent when the server has too little history; one that is not an integer from 1 to 2^53−1 is read as absent for
  that item and refuses nothing.
  A thin price, one resting on so few units (5 or fewer) that one listing can move it, also carries
  `"units"`, the units on sale in its newest pooled scan; an older server sends none. One that is not
  an integer from 0 to 2^53−1 is read as absent for that item and refuses nothing.
- An answer over 4 MB (retail: 8 MB), or one whose `Data.lua` would be over 1 MB (retail: 4 MB), is
  refused and the last good file stays; a market taking over 200 KB of it (retail: 1 MB) is written
  with a warning. Retail's markets are realm groups of 8–13k items.
- 304, no body: the prices are unchanged; nothing is written.
- 401 (a wrong or revoked token), 403 (a suspended account, or a token without `prices:read`), 404
  and 400 write nothing; the next try is in an hour.
- 5xx or no answer writes nothing and is tried again after 1, 2, 4 … up to 30 minutes, as uploads
  are (a server still loading its prices after a start answers 503 with `Retry-After: 15`); 429 after
  `Retry-After`.

The app then writes `Interface/AddOns/GoldForever_Data/` in each flavour folder of the game: a TOC with
the interface number its save names, and `Data.lua`, one table of integers under quoted market keys
(`[itemId] = { value, minBuyout, players, scannedAt }`, with `week` fifth when the item has one and
`units = <n>` after the numbers when its price is thin; neither changes `v`),
each through a temporary file renamed into place. The server never sends Lua, so a compromised server
could send wrong prices but could never run code in your game.

## The dings switch

With every price refresh, one request for the site's side of the addon's *Announce dings on Discord*
switch, the same for every game of the account:

```http
GET /api/v1/dings/state
Authorization: Bearer gf_...
If-None-Match: "<the ETag of the dings state held>"
```

- 200 `{"v":1,"at":<epoch s>,"webhook":true,"characters":{"Player-4620-006C0FC1":true}}`: when the
  state last changed, whether a level-up webhook is connected, and each character's choice. The app
  checks it (version 1, `at` a plain integer, booleans only, no key twice); any fault, a state without
  `at` too, keeps the last good state (or writes none), as does a failed request. A character whose key
  is not of the form `Player-<digits>-<hex>` is only left out: the others are written, and the log
  says so once for that state. It is kept with its ETag in `prices.json`.
- 304: the state held is still current.
- 404: historical, from servers before the dings state; still accepted as no state: `Data.lua` goes
  without one, and the addon leaves its switch changeable.

`Data.lua` carries it beside `at` as `dings = { at = <n>, webhook = true, characters = { ["Player-…"] =
true } }`; the addon compares its own choices against `dings.at`.
A changed state rewrites `Data.lua` even when the prices answer 304; after a restart, when the prices
written are no longer in memory, that one refresh asks for the prices without `If-None-Match`.

## Shopping lists and sale profiles

With every price refresh, one request for the account's shopping lists and sale profiles, the same for
every game of the account:

```http
GET /api/v1/shared/state
Authorization: Bearer gf_...
If-None-Match: "<the ETag of the shared state held>"
```

- 200 `{"v":1,"at":<epoch s>,"lists":[{"id":"…","edited":<epoch s>,"string":"GFL1:…"}],"profiles":[{"id":"…","edited":<epoch s>,"string":"GFP1:…"}]}`:
  when the account's set last changed, and each list and profile with its id and last edit. The app
  checks it (version 1, plain integers, at most 50 lists and 50 profiles, ids of 1–64 `A-Z a-z 0-9 _ -`
  none twice in a kind, strings starting `GFL1:` or `GFP1:` of printable ASCII only); any fault keeps
  the last good state (or writes none), as does a failed request. Up to 1 MB; it is kept with its ETag
  in `prices.json`. 401 and 403 are as for prices.
- 304: the state held is still current.
- 404: historical, from servers before shared lists and profiles; still accepted as no state:
  `Data.lua` goes without one, and the addon keeps its own.

`Data.lua` carries it beside `dings` as `shared = { at = <n>, lists = { { id = "…", edited = <n>,
string = "GFL1:…" } }, profiles = { { id = "…", edited = <n>, string = "GFP1:…" } } }`, counted in the
game's file limit; the addon merges it by id, the newer `edited` winning. A changed state rewrites
`Data.lua` even when the prices answer 304, as the dings state does.
