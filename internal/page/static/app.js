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
  const notice = $("notice");
  notice.hidden = !s.notice;
  if (s.notice) {
    notice.replaceChildren(`Version ${s.notice.version} is out. `);
    const a = document.createElement("a");
    a.href = s.notice.url; a.textContent = "Download it"; a.target = "_blank"; a.rel = "noreferrer";
    notice.append(a, " and run it: it replaces this one.");
  }
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
  $("saves").replaceChildren(...(s.saves || []).map((save) => {
    const box = document.createElement("input");
    box.type = "checkbox"; box.checked = save.on;
    box.onchange = () => act(call("/api/save", { path: save.path, on: box.checked }));
    const game = document.createElement("span");
    game.className = "game"; game.textContent = ` (${save.game})`;
    const label = document.createElement("label");
    label.append(box, " ", save.label, game);
    return li(label);
  }));
  if (!(s.saves || []).length) $("saves").replaceChildren(li("None yet. Log in to World of Warcraft once with the Gold Forever addon, or add your WoW folder below."));
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

function showHistory(h) {
  $("log").replaceChildren(...(h.entries || []).slice().reverse().map((e) => {
    const row = document.createElement("tr");
    for (const text of [new Date(e.time).toLocaleString(), e.save, `${Math.round(e.size / 1024)} KB`, e.answer]) {
      const cell = document.createElement("td");
      cell.textContent = text;
      row.append(cell);
    }
    return row;
  }));
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
  showState(await call("/api/state"));
  showHistory(await call("/api/history"));
}

$("autostart").onchange = () => act(call("/api/autostart", { on: $("autostart").checked }));
$("welcomed").onclick = () => act(call("/api/welcomed", {}));
$("device").onclick = () => act(call("/api/link/device", {}));
$("link").onsubmit = (e) => { e.preventDefault(); act(call("/api/link", { token: $("token").value.trim() })); $("token").value = ""; };
$("unlink").onclick = () => act(call("/api/unlink", {}));
$("folder").onsubmit = (e) => { e.preventDefault(); act(call("/api/folder", { path: $("path").value.trim() })); $("path").value = ""; };
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
