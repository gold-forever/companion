// The settings page. Every call goes to this companion only; the secret travels in a cookie.
"use strict";

const $ = (id) => document.getElementById(id);

async function call(path, body) {
  const options = body === undefined ? {} : { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) };
  const res = await fetch(path, options);
  if (!res.ok) {
    let message = res.statusText;
    try { message = (await res.json()).error || message; } catch { /* a plain-text answer */ }
    throw new Error(message);
  }
  return res.json();
}

function act(promise) {
  promise.then(refresh, (e) => alert(e.message));
}

function li(...children) {
  const item = document.createElement("li");
  item.append(...children);
  return item;
}

function button(text, onclick) {
  const b = document.createElement("button");
  b.textContent = text;
  b.className = "quiet";
  b.onclick = onclick;
  return b;
}

function showState(s) {
  $("status").textContent = s.status || (s.linked ? "Watching for new saves." : "Not linked yet.");
  showBanner(s.notice, s.update || {});
  $("warning").hidden = !s.warning;
  $("warning").textContent = s.warning || "";
  $("sharing").textContent = s.sharing;
  $("welcome").hidden = !s.firstRun;
  $("installed").replaceChildren(...(s.installed || []).map((line) => li(line)));
  $("autostart").checked = s.autostart;
  showLinking(s.linking, s.linked);
  $("unlinked").hidden = s.linked;
  $("linked").hidden = !s.linked;
  $("tokens").href = s.site + "/me/tokens";
  $("revoke").href = s.site + "/me/tokens";
  $("site").href = s.site; $("site").textContent = s.site;
  $("uploading-to").hidden = !s.account;
  $("account").textContent = s.account || "";
  $("saves").replaceChildren(...(s.saves || []).map((save) => {
    const box = document.createElement("input");
    box.type = "checkbox"; box.checked = save.on;
    box.onchange = () => act(call("/api/save", { path: save.path, on: box.checked }));
    const game = document.createElement("span");
    game.className = "game"; game.textContent = ` (${save.game})`;
    const label = document.createElement("label");
    label.append(box, " ", save.label, game);
    if (save.note) {
      const note = document.createElement("span");
      note.className = "game"; note.textContent = ` — ${save.note}`;
      label.append(note);
    }
    return li(label);
  }));
  if (!(s.saves || []).length) $("saves").replaceChildren(li("None yet. Log in to World of Warcraft once with the Gold Forever addon, or add your WoW folder below."));
  showPrices(s.prices || []);
  showUpdate(s.version, s.update || {});
}

function releaseLink(url, text) {
  const a = document.createElement("a");
  a.href = url; a.textContent = text; a.target = "_blank"; a.rel = "noreferrer";
  return a;
}

// The version this page's Update started from, once the new version has started: it replaces this
// companion and answers this tab at the same address, which then reloads onto it.
let updatedFrom = "";

// The banner on top of the page while a newer release is known: Update now where the companion can
// install it itself, else the release's page.
function showBanner(n, u) {
  $("banner").hidden = !n;
  if (!n) return;
  const text = $("banner-text");
  if (u.installing) text.replaceChildren(`Updating to version ${n.version}…`);
  else if (u.canInstall) text.replaceChildren(`Version ${n.version} is out. `, releaseLink(n.url, "What's new"));
  else text.replaceChildren(`Version ${n.version} is out: `, releaseLink(n.url, "download it"), " and run it, it replaces this one.");
  $("update-now").hidden = !u.canInstall;
  $("update-now").disabled = u.installing;
}

function update() {
  $("update").disabled = true;
  $("update-now").disabled = true;
  $("updating").hidden = false;
  const from = $("running").textContent;
  call("/api/update", {}).then(() => { updatedFrom = from; return refresh().finally(awaitNewVersion); }, refresh);
}

// Asks every second until the new version answers at this address, and refresh reloads onto it.
function awaitNewVersion() {
  setTimeout(() => refresh().then(awaitNewVersion, awaitNewVersion), 1000);
}

// The update section: the running version, the latest release the last check found, and Update where
// the companion can install it itself.
function showUpdate(version, u) {
  $("running").textContent = version;
  const latest = $("latest");
  if (!u.latest) latest.replaceChildren("");
  else if (!u.newer) latest.replaceChildren(`It is the latest release (${u.latest}).`);
  else {
    latest.replaceChildren(`Version ${u.latest} is out. `, releaseLink(u.url, "What's new"));
    if (!u.canInstall) latest.append(": download it there and run it, it replaces this one.");
  }
  $("update").hidden = !u.newer || !u.canInstall;
  $("update").disabled = u.installing;
  $("check").disabled = u.installing;
  $("updating").hidden = !u.installing;
  $("updating").textContent = updatedFrom
    ? "The new version is installing itself: this page reloads onto it when it has started."
    : "Downloading the update and checking it against the release's checksum…";
  const error = $("update-error");
  error.hidden = !u.error;
  error.replaceChildren(u.error || "");
  if (u.error && u.url) error.append(" ", releaseLink(u.url, "Download it from the release page"), " instead.");
}

function row(...texts) {
  const r = document.createElement("tr");
  for (const text of texts) {
    const cell = document.createElement("td");
    cell.textContent = text;
    r.append(cell);
  }
  return r;
}

// Each flavour's price file: what it holds and how the latest fetch of its game went.
function showPrices(files) {
  $("prices").replaceChildren(...files.map((f) => {
    const markets = (f.markets || []).map((m) => `${m.market} (${m.items})`).join(", ") || "none";
    const size = f.size ? `${Math.round(f.size / 1024)} KB` : "no file";
    let fetched = "not yet";
    if (f.fetched) {
      fetched = `${new Date(f.fetched).toLocaleString()} · ${f.outcome === "error" ? "error: " + f.error : f.outcome}`;
    }
    const r = row(`${f.flavour} (${f.game})`, markets, String(f.items), size, fetched);
    for (const text of [f.dings, f.shared]) {
      if (!text) continue;
      const line = document.createElement("div");
      line.className = "hint";
      line.textContent = text;
      r.firstChild.append(line);
    }
    if (f.outcome === "error") r.className = "failed";
    return r;
  }));
  if (!files.length) $("prices").replaceChildren(row("No Forever or retail game folder found yet.", "", "", "", ""));
}

// A link by code: the code to approve while it waits, or why it ended.
function showLinking(l, linked) {
  const p = $("linking");
  const waiting = !!l && (l.state === "starting" || l.state === "waiting");
  $("device").disabled = waiting;
  p.hidden = !l || linked;
  if (!l) return;
  if (l.state === "starting") {
    p.replaceChildren("Asking Gold Forever for a code…");
  } else if (l.state === "waiting") {
    const code = document.createElement("strong");
    code.textContent = l.userCode;
    const a = document.createElement("a");
    a.href = l.url; a.textContent = "open it here"; a.target = "_blank"; a.rel = "noreferrer";
    p.replaceChildren("Approve the code ", code, " in your browser. If no page opened, ", a, ".");
  } else {
    const why = { denied: "The link was denied.", expired: "The code expired before it was approved. If the site said an account keeps at most 20 tokens, revoke one you no longer use on your tokens page, then try again.", failed: "Linking failed: " + (l.error || "unknown error") + "." };
    p.replaceChildren((why[l.state] || "") + " Press the button to try again.");
  }
  schedule(waiting ? 1500 : 5000);
}

// The page of "What was sent" the player is on: it stays while new uploads arrive, which come in on page 1.
let logPage = 1;

function showHistory(h) {
  logPage = h.page || 1;
  $("log").replaceChildren(...(h.entries || []).map((e) =>
    row(new Date(e.time).toLocaleString(), e.save, `${Math.round(e.size / 1024)} KB`, e.answer)));
  $("log-pages").hidden = (h.pages || 1) < 2;
  $("log-page").textContent = `page ${logPage} of ${h.pages || 1}`;
  $("newer").disabled = logPage <= 1;
  $("older").disabled = logPage >= (h.pages || 1);
  $("copies").replaceChildren(...(h.copies || []).slice().reverse().map((c) => {
    const view = button("View", () => window.open("/api/copy?id=" + encodeURIComponent(c.id), "_blank"));
    const restore = button("Restore", () => {
      if (confirm("Is World of Warcraft closed? The game overwrites the save when you log out, so restore only while it is closed.")) {
        act(call("/api/restore", { id: c.id, wowClosed: true }));
      }
    });
    return li(`${new Date(c.taken).toLocaleString()} · ${c.save}${c.pending ? " · not sent yet" : ""} `, view, " ", restore);
  }));
}

async function refresh() {
  const s = await call("/api/state");
  if (updatedFrom && s.version !== updatedFrom) {
    location.reload();
    return;
  }
  showState(s);
  showHistory(await call("/api/history?page=" + logPage));
}

$("autostart").onchange = () => act(call("/api/autostart", { on: $("autostart").checked }));
$("welcomed").onclick = () => act(call("/api/welcomed", {}));
$("device").onclick = () => act(call("/api/link/device", {}));
$("link").onsubmit = (e) => { e.preventDefault(); act(call("/api/link", { token: $("token").value.trim() })); $("token").value = ""; };
$("unlink").onclick = () => act(call("/api/unlink", {}));
$("folder").onsubmit = (e) => { e.preventDefault(); act(call("/api/folder", { path: $("path").value.trim() })); $("path").value = ""; };
$("newer").onclick = () => { logPage--; refresh(); };
$("older").onclick = () => { logPage++; refresh(); };
$("check").onclick = () => call("/api/update/check", {}).then(refresh, refresh);
$("update").onclick = update;
$("update-now").onclick = update;
$("uninstall").onclick = () => {
  if (!confirm("Uninstall the Gold Forever Companion from this computer?")) return;
  call("/api/uninstall", { keepData: $("keep").checked }).then(
    () => { document.body.replaceChildren("The companion is uninstalled. You can close this page."); },
    (e) => alert(e.message));
};

// Refreshes every 5 s, and faster while a link waits for its approval.
let timer;
function schedule(ms) {
  clearTimeout(timer);
  timer = setTimeout(() => refresh().catch(() => schedule(5000)), ms);
}

refresh().catch((e) => { $("status").textContent = e.message; schedule(5000); });
