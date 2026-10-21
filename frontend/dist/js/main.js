// ---------------- действия ----------------

async function guarded(fn) {
  if (busy) return;
  busy = true;
  try { return await fn(); } finally { busy = false; }
}

// Разделы открываются и с главной, и из настроек; «назад» ведёт туда,
// откуда пришли.
let returnTo = "viewMain";

function show(id) {
  for (const v of document.querySelectorAll(".view")) v.hidden = v.id !== id;
  $("btnSettings").classList.toggle("active", id === "viewSettings" || id === "viewLog");
  if (id === "viewLog") { const l = $("log"); l.scrollTop = l.scrollHeight; }
  if (id === "viewVpn" && S && S.vpn.hasSub) api().PingServers();
}

function openSection(id, from) {
  returnTo = from;
  show(id);
}

// окно
$("btnMin").addEventListener("click", () => window.runtime.WindowMinimise());
$("btnClose").addEventListener("click", () => window.runtime.WindowHide());
$("btnSettings").addEventListener("click", () => show($("viewSettings").hidden ? "viewSettings" : "viewMain"));
for (const b of document.querySelectorAll("[data-back]")) {
  b.addEventListener("click", () => {
    const to = b.dataset.back;
    show(to === "return" ? returnTo : to === "listFrom" ? listFrom : to);
  });
}
for (const b of document.querySelectorAll("[data-open]")) b.addEventListener("click", () => show(b.dataset.open));
for (const b of document.querySelectorAll("[data-section]")) b.addEventListener("click", () => openSection(b.dataset.section, "viewSettings"));

// главная
// Выключение не ждёт идущего включения: бэкенд сам отменит запуск. Иначе
// флажок «занято» молча глотал бы клик «Выключить».
const offOrGuarded = (on, fn) => (on ? guarded(fn) : fn());
$("toggle").addEventListener("click", () => {
  const st = $("toggle").dataset.status;
  offOrGuarded(st === "stopped" || st === "error", () => api().Toggle());
});
$("modZapret").addEventListener("click", (e) => { if (!e.target.closest(".switch")) openSection("viewZapret", "viewMain"); });
$("modVpn").addEventListener("click", (e) => { if (!e.target.closest(".switch")) openSection("viewVpn", "viewMain"); });
$("modTg").addEventListener("click", (e) => { if (!e.target.closest(".switch")) openSection("viewTg", "viewMain"); });
$("swZapret").addEventListener("change", (e) => offOrGuarded(e.target.checked, () => api().SetZapret(e.target.checked)));
$("swVpn").addEventListener("change", (e) => offOrGuarded(e.target.checked, () => api().SetVPN(e.target.checked)));
$("swTg").addEventListener("change", (e) => guarded(async () => { const r = await api().SetTelegram(e.target.checked); if (r) notify(r); }));
$("tgConnect").addEventListener("click", () => guarded(async () => { $("tgErr").textContent = await api().ConnectTelegram(); }));
$("tgCopy").addEventListener("click", async () => {
  const link = await api().TelegramLink();
  await window.runtime.ClipboardSetText(link);
  $("tgCopyLabel").textContent = t("Скопировано");
  setTimeout(() => { $("tgCopyLabel").textContent = ""; }, 2500);
});

// VPN
$("subPaste").addEventListener("click", async () => {
  const txt = await window.runtime.ClipboardGetText();
  if (txt) $("subInput").value = txt.trim();
});
$("subAdd").addEventListener("click", () => guarded(async () => {
  const url = $("subInput").value.trim();
  const btn = $("subAdd"), err = $("subErr");
  err.textContent = "";
  if (!url) { err.textContent = t("Вставьте ссылку на подписку"); return; }
  btn.disabled = true;
  btn.textContent = t("Загружаю…");
  const res = await api().AddSubscription(url);
  btn.disabled = false;
  btn.textContent = t("Добавить");
  if (res) err.textContent = res;
  else { $("subInput").value = ""; addMode = false; render(S); }
}));
$("subCancel").addEventListener("click", () => { addMode = false; $("subErr").textContent = ""; render(S); });
$("subRefresh").addEventListener("click", () => guarded(() => api().RefreshSubscription()));
$("subRemove").addEventListener("click", async () => {
  const v = S.vpn;
  const cur = (v.subs || []).find((x) => x.active);
  const name = cur ? "«" + cur.title + "»" : "";
  const rest = t((v.subs || []).length > 1 ? "VPN переключится на другую подписку." : "VPN отключится.");
  if (await ask(t("Удалить подписку {x}? {y}", name, rest), { ok: t("Удалить"), danger: true })) api().RemoveSubscription(v.activeSub);
});
$("subSupport").addEventListener("click", () => api().OpenURL(S.vpn.info.supportUrl));
$("subWeb").addEventListener("click", () => api().OpenURL(S.vpn.info.webPage));
for (const b of $("vpnMode").children) b.addEventListener("click", () => guarded(() => api().SetVPNMode(b.dataset.v)));
for (const b of $("vpnRoute").children) b.addEventListener("click", () => guarded(() => api().SetVPNRoute(b.dataset.v)));
$("pingBtn").addEventListener("click", async () => {
  const b = $("pingBtn");
  b.disabled = true;
  b.textContent = t("Меряю…");
  await api().PingServers();
  b.disabled = false;
  b.textContent = t("Проверить пинг");
});
$("sendHwid").addEventListener("change", (e) => guarded(async () => {
  const r = await api().SetSendHWID(e.target.checked);
  if (r) notify(r);
}));
$("updateBtn").addEventListener("click", () => api().InstallAppUpdate());
$("updatePage").addEventListener("click", () => api().OpenAppUpdate());
$("vpnCheckBtn").addEventListener("click", () => api().CheckVPN());

// обход
$("pickBtn").addEventListener("click", async () => {
  if (S && S.pick && S.pick.running) { api().StopPickStrategy(); return; }
  if (S && ["running", "starting"].includes(S.vpn.status) &&
      !(await ask(t("Для подбора нужен обход — VPN будет выключен. Продолжить?"), { ok: t("Продолжить") }))) return;
  api().AutoPickStrategy();
});
$("recheck").addEventListener("click", () => api().RunChecks());
for (const b of $("gameFilter").children) b.addEventListener("click", () => guarded(() => api().SetGameFilter(b.dataset.v)));
for (const b of $("ipset").children) b.addEventListener("click", () => guarded(async () => {
  const r = await api().SetIPSetMode(b.dataset.v);
  if (r) notify(r);
}));
$("ipsetUpdate").addEventListener("click", () => guarded(async () => {
  const label = $("ipsetUpdateLabel");
  label.textContent = t("Скачиваю…");
  const r = await api().UpdateIPSet();
  label.textContent = "";
  if (r.error) return notify(t("Список адресов не обновился: {x}", r.error));
  const off = S && S.zapret.ipset;
  notify(t("Список адресов обновлён, записей: {x}.", r.count) +
    (off ? " " + t("Он работает в режиме «По списку».") : ""));
}));
$("hostsBlock").addEventListener("change", (e) => guarded(async () => {
  const on = e.target.checked;
  if (on && !(await ask(t("Починка добавит в файл hosts адреса Discord и Telegram из zapret-discord-youtube. Они закрепляют сайты за конкретными адресами: если адрес сменится, сайт не откроется до следующего обновления (при запуске Починки). Выключение убирает только эти записи."), { ok: t("Добавить") }))) {
    e.target.checked = false;
    return;
  }
  const r = await api().SetHostsBlock(on);
  if (r) { e.target.checked = !on; notify(r); }
}));
$("autoUpdate").addEventListener("change", (e) => api().SetAutoUpdate(e.target.checked));
$("checkUpd").addEventListener("click", async () => {
  const lbl = $("checkUpdLabel");
  if (lbl.dataset.busy) return;
  lbl.dataset.busy = "1";
  lbl.textContent = t("Проверяю…");
  lbl.textContent = await api().CheckUpdates();
  setTimeout(() => { lbl.textContent = t("Проверить"); delete lbl.dataset.busy; }, 4000);
});

// настройки приложения
$("themeEdit").addEventListener("click", openThemeEditor);
for (const b of $("themeScheme").children) b.addEventListener("click", () => { draft.scheme = b.dataset.v; renderThemeEditor(); });
$("themeSave").addEventListener("click", () => guarded(async () => {
  if (!validPalette(draft)) return;
  const p = { scheme: draft.scheme };
  for (const k of THEME_KEYS) p[k] = draft[k];
  const r = await api().SetCustomTheme(p);
  if (r) { notify(r); return; }
  CUSTOM = p;
  if (S) { S.config.customTheme = p; S.config.theme = "custom"; }
  applyTheme("custom");
  show("viewSettings");
}));
for (const b of $("lang").children) b.addEventListener("click", () => setLang(b.dataset.v, true));
$("openLogs").addEventListener("click", () => api().OpenLogsFolder());
$("autostart").addEventListener("change", (e) => guarded(async () => {
  const r = await api().SetAutostart(e.target.checked);
  if (r) { notify(r); if (S) render(S); }
}));
$("checkUpdate").addEventListener("click", () => guarded(async () => {
  const label = $("checkUpdateLabel");
  label.textContent = t("Проверяю…");
  const r = await api().CheckAppUpdate();
  label.textContent = "";
  if (r.error) return notify(t("Не удалось проверить обновления: {x}", r.error));
  if (!r.version) return notify(t("У вас последняя версия: {x}", S ? S.version : ""));
  if (await ask(t("Доступна версия {x}. Обновить сейчас?", r.version), { ok: t("Обновить"), cancel: t("Позже") })) {
    show("viewMain");
    api().InstallAppUpdate();
  }
}));
$("licenses").addEventListener("click", () => api().OpenLicenses());
$("uninstall").addEventListener("click", async () => {
  if (!(await ask(t("Удалить Починку? Будут выключены обход, VPN и прокси для Telegram, возвращены настройки прокси Windows и удалены: копия программы, подписки, настройки и журналы. Скачанный вами exe останется — его можно удалить вручную."), { ok: t("Удалить"), danger: true }))) return;
  const r = await api().Uninstall();
  if (r) notify(r);
});
$("copyReport").addEventListener("click", async () => {
  await window.runtime.ClipboardSetText(await api().SupportReport());
  $("copyReportLabel").textContent = t("Скопировано");
  setTimeout(() => { $("copyReportLabel").textContent = ""; }, 2500);
});
for (const b of $("sdns").children) b.addEventListener("click", () => guarded(async () => {
  const r = await api().SetSecureDNS(b.dataset.v);
  if (r) notify(r);
}));
$("dnsFix").addEventListener("click", () => guarded(async () => {
  const r = await api().SetSecureDNS("google");
  if (r) notify(r);
}));
$("quit").addEventListener("click", () => api().Quit());

// журнал
function appendLog(line) {
  const pre = $("log");
  const atBottom = pre.scrollTop + pre.clientHeight >= pre.scrollHeight - 4;
  pre.textContent += line + "\n";
  if (pre.textContent.length > 60000) pre.textContent = pre.textContent.slice(-40000);
  if (atBottom) pre.scrollTop = pre.scrollHeight;
}

translateStatic(); // язык из localStorage — до первого состояния, без мигания
window.addEventListener("DOMContentLoaded", async () => {
  window.runtime.EventsOn("state", render);
  window.runtime.EventsOn("log", appendLog);
  render(await api().GetState());
  for (const l of await api().GetLog()) appendLog(l);
});
