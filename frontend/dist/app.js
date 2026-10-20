// Связь с Go: window.go.main.App.* — методы, window.runtime.* — окно и события.
// Перевод (t, plural, translateStatic, LANG) — в i18n.js.
const api = () => window.go.main.App;
const $ = (id) => document.getElementById(id);

const CHECK_SVG = '<svg class="check" viewBox="0 0 16 16"><path d="M3.5 8.5 6.5 11.5 12.5 5"/></svg>';
const ACTIVE = ["running", "starting", "installing"];

let S = null;     // последнее состояние
let busy = false; // защита от двойных кликов

// "general (ALT2)" → "ALT2", "general" → "Стандартная"
const prettyName = (n) => n.replace(/^general\s*/, "").replace(/^\((.*)\)$/, "$1") || t("Стандартная");

function fmtBytes(n) {
  const u = LANG === "en" ? ["B", "KB", "MB", "GB", "TB"] : ["Б", "КБ", "МБ", "ГБ", "ТБ"];
  if (!n) return "0 " + u[0];
  const i = Math.min(u.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
  const v = n / Math.pow(1024, i);
  const s = v >= 100 || i === 0 ? v.toFixed(0) : v.toFixed(1);
  return (LANG === "en" ? s : s.replace(".", ",")) + " " + u[i];
}

function pingClass(p) {
  if (p > 0 && p < 120) return "good";
  if (p > 0 && p < 300) return "mid";
  if (p < 0) return "bad";
  return "";
}

function pingText(sv) {
  if (sv.ping > 0) return t("{x} мс", sv.ping);
  if (sv.ping < 0) return t("не отвечает");
  return "";
}

// ---------------- общий статус ----------------

function overall(s) {
  const z = s.zapret.status, v = s.vpn.status;
  if (z === "error" || v === "error") return "error";
  if (z === "starting" || z === "installing" || v === "starting") return "starting";
  if (z === "running" || v === "running") return "running";
  return "stopped";
}

function render(s) {
  S = s;
  const z = s.zapret, v = s.vpn;
  $("adminNote").hidden = s.admin;
  const up = s.update || {};
  $("updateNote").hidden = !up.version;
  $("updateNote").classList.toggle("warn", !!up.error);
  $("updateText").textContent = !up.version ? ""
    : up.busy ? t("Обновляю до {x}… {y}%", up.version, up.progress || 0)
    : up.error ? up.error
    : t("Доступна версия {x}", up.version);
  $("updateBtn").hidden = !!up.busy;
  $("updateBtn").textContent = t(up.error ? "Ещё раз" : "Обновить");
  $("updatePage").hidden = !!up.busy;
  $("updatePage").textContent = t(up.error ? "Скачать вручную" : "Что нового");
  $("conflictNote").hidden = !s.conflict;
  $("conflictNote").textContent = s.conflict
    ? t("Работает другой VPN — «{x}». Выключите его: два VPN мешают друг другу, и сайты не грузятся.", s.conflict)
    : "";

  // ----- главная кнопка -----
  const st = overall(s);
  const tg = $("toggle");
  tg.dataset.status = st;
  tg.setAttribute("aria-label", st === "stopped" ? t("Подключить") : t("Отключить"));
  const title = { stopped: "Отключено", starting: "Подключение", running: "Подключено", error: "Ошибка" }[st];
  $("stateTitle").textContent = t(title);
  const sub = $("stateSub");
  sub.classList.toggle("err", st === "error");
  if (st === "error") sub.textContent = z.status === "error" ? z.error : v.error;
  else if (z.busy || v.busy) sub.textContent = z.busy || v.busy;
  else if (st === "running") {
    const parts = [];
    if (z.status === "running") parts.push("zapret");
    if (v.status === "running") parts.push("VPN");
    sub.textContent = t(parts.length === 2 ? "Обход и VPN работают" : parts[0] === "VPN" ? "VPN работает" : "Обход работает");
  } else if (st === "starting") sub.textContent = t("Секунду…");
  else sub.textContent = t("Нажмите, чтобы подключиться");

  renderZapretModule(s);
  renderVpnModule(s);
  renderTg(s);
  renderVpnView(s);
  renderZapretView(s);
  renderSettings(s);
}

function renderZapretModule(s) {
  const z = s.zapret;
  const mod = $("modZapret"), sub = $("zapretSub");
  $("swZapret").checked = ACTIVE.includes(z.status);
  mod.className = "module link" + (z.status === "running" ? " on" : z.status === "error" ? " err" : "");
  sub.className = "";
  if (z.status === "error") sub.textContent = z.error;
  else if (z.busy) sub.textContent = z.busy;
  else if (z.status === "starting" || z.status === "installing") sub.textContent = t("Запускаю…");
  else if (z.status === "running") {
    if (s.checking) sub.textContent = t("Проверяю Discord и YouTube…");
    else {
      const d = s.checks.discord, y = s.checks.youtube;
      if (d && y) {
        const bad = [d.ok ? null : "Discord", y.ok ? null : "YouTube"].filter(Boolean);
        if (bad.length === 0) { sub.textContent = t("Discord и YouTube открываются"); sub.className = "okline"; }
        else sub.textContent = t("{x} не открывается — смените стратегию", bad.join(t(" и ")));
      } else sub.textContent = t("Стратегия: {x}", prettyName(z.strategy));
    }
  } else sub.textContent = ["running", "starting"].includes(s.vpn.status) ? t("Выключен — сейчас работает VPN") : t("Discord, YouTube, игры");
}

function serverName(v, id) {
  if (id === "auto") return t("Авто");
  const sv = (v.servers || []).find((x) => x.id === id);
  return sv ? sv.name : t("Авто");
}

function renderTg(s) {
  const tg = s.tg || {};
  const st = tg.stats || {};
  const mod = $("modTg"), sub = $("tgSub");
  $("swTg").checked = !!tg.running;
  mod.className = "module link" + (tg.running && st.active > 0 ? " on" : tg.error ? " err" : "");
  sub.className = "";
  if (tg.error && !tg.running) sub.textContent = tg.error;
  else if (!tg.installed) sub.textContent = t("Telegram Desktop не найден");
  else if (!tg.running) sub.textContent = t("Прокси для Telegram Desktop");
  else if (st.active > 0) { sub.textContent = t("Подключён · соединений: {x}", st.active); sub.className = "okline"; }
  else if (st.total > 0) sub.textContent = t("Прокси работает · Telegram не подключён");
  else sub.textContent = t("Прокси готов — нажмите «Подключить Telegram»");

  $("tgAddr").textContent = "127.0.0.1:" + (tg.port || 1443);
  const pill = $("tgPill");
  pill.className = "pill" + (tg.running ? (st.active > 0 ? " on" : "") : "");
  pill.textContent = t(!tg.running ? "выключен" : st.active > 0 ? "подключён" : "ожидает Telegram");
  $("tgConnect").disabled = !tg.installed;
  $("tgHint").textContent = tg.installed
    ? t("Telegram спросит «Подключить прокси?» — нажмите «Подключить».")
    : t("Установите Telegram Desktop с desktop.telegram.org, затем нажмите кнопку.");
  $("tgActive").textContent = tg.running ? t("{x} (всего {y})", st.active || 0, st.total || 0) : "—";
  $("tgPaths").textContent = tg.running ? (st.viaWs || 0) + " / " + (st.viaCf || 0) + " / " + (st.viaTcp || 0) : "—";
  $("tgTraffic").textContent = tg.running ? fmtBytes(st.bytesUp || 0) + " / " + fmtBytes(st.bytesDown || 0) : "—";
}

function renderVpnModule(s) {
  const v = s.vpn;
  const mod = $("modVpn"), sub = $("vpnSub"), sw = $("swVpn");
  sw.disabled = !v.hasSub;
  sw.checked = v.status === "running" || v.status === "starting";
  mod.className = "module link" + (v.status === "running" ? " on" : v.status === "error" ? " err" : "");
  if (!v.hasSub) sub.textContent = t("Добавить подписку");
  else if (v.status === "error") sub.textContent = v.error;
  else if (v.busy) sub.textContent = v.busy;
  else if (v.status === "starting") sub.textContent = t("Подключаюсь…");
  else if (v.status === "running") {
    const sv = (v.servers || []).find((x) => x.name === v.active);
    const ping = sv && sv.ping > 0 ? " · " + t("{x} мс", sv.ping) : "";
    sub.textContent = (v.active || t("Авто")) + ping + (v.mode === "tun" ? " · TUN" : "");
    // Самопроверка не прошла — показываем причину прямо на главной.
    if (v.check && !v.checkOk && !v.check.startsWith("Проверяю")) {
      sub.textContent = v.check;
      mod.className = "module link err";
    }
  } else sub.textContent = ACTIVE.includes(s.zapret.status) ? t("Выключен — сейчас работает обход") : t("{x} · выключен", serverName(v, v.selected));
}

// ---------------- VPN ----------------

let addMode = false; // открыта форма добавления ещё одной подписки

function renderSubTabs(v) {
  const box = $("subTabs");
  box.hidden = !v.hasSub;
  if (!v.hasSub) return;
  const sig = (v.subs || []).map((x) => x.id + ":" + x.title + ":" + x.active).join("|") + "|" + addMode + "|" + LANG;
  if (box.dataset.sig === sig) return;
  box.dataset.sig = sig;
  box.innerHTML = "";
  for (const sub of v.subs || []) {
    const b = document.createElement("button");
    b.className = "chip" + (sub.active && !addMode ? " sel" : "");
    b.title = sub.host + " · " + t("серверов: {x}", sub.servers);
    b.innerHTML = "<span></span>";
    b.firstChild.textContent = sub.title;
    b.addEventListener("click", () => {
      addMode = false;
      if (!sub.active) guarded(() => api().SetActiveSub(sub.id));
      render(S);
    });
    box.appendChild(b);
  }
  const add = document.createElement("button");
  add.className = "chip add" + (addMode ? " sel" : "");
  add.title = t("Добавить подписку");
  add.textContent = "+";
  add.addEventListener("click", () => { addMode = true; $("subErr").textContent = ""; render(S); $("subInput").focus(); });
  box.appendChild(add);
}

function renderVpnView(s) {
  const v = s.vpn;
  // HWID — до раннего выхода: переключатель нужен и без подписки.
  const hw = !s.config.vpnNoHwid;
  $("sendHwid").checked = hw;
  $("hwidHint").textContent = hw
    ? t("HWID этого компьютера: {x}", v.hwid || "—")
    : t("Панель не узнает это устройство. Подписка с лимитом устройств может не загрузиться.");
  if (!v.hasSub) addMode = false;
  renderSubTabs(v);
  $("subEmpty").hidden = v.hasSub && !addMode;
  $("subCancel").hidden = !v.hasSub;
  $("subFull").hidden = !v.hasSub || addMode;
  $("subRefresh").hidden = !v.hasSub;
  $("subRefresh").classList.toggle("spin", !!v.busy);
  if (!v.hasSub || addMode) return;

  const info = v.info || {};
  $("subTitle").textContent = info.title || t("Подписка");
  $("subHost").textContent = v.subHost || "";

  const pill = $("vpnPill");
  pill.className = "pill" + (v.status === "running" ? " on" : v.status === "error" ? " err" : v.status === "starting" ? " wait" : "");
  pill.textContent = t({ running: "подключено", starting: "подключаюсь", error: "ошибка", stopped: "выключено" }[v.status] || "");

  // трафик
  const used = (info.upload || 0) + (info.download || 0);
  const bar = $("trafficBar");
  $("trafficBox").hidden = !used && !info.total && !info.expire;
  if (info.total > 0) {
    const pct = Math.min(100, (used / info.total) * 100);
    bar.style.width = pct.toFixed(1) + "%";
    bar.className = pct > 90 ? "bad" : pct > 75 ? "warn" : "";
    $("trafficText").textContent = t("{x} из {y}", fmtBytes(used), fmtBytes(info.total));
  } else {
    bar.style.width = "100%";
    bar.className = "";
    $("trafficText").textContent = used ? t("{x} · безлимит", fmtBytes(used)) : t("Безлимит");
  }
  if (info.expire > 0) {
    const days = Math.ceil((info.expire * 1000 - Date.now()) / 86400000);
    const date = new Date(info.expire * 1000).toLocaleDateString(LANG === "en" ? "en-GB" : "ru-RU", { day: "numeric", month: "long" });
    $("expireText").textContent = days <= 0 ? t("истекла") : t("до {x} · {y}", date, days + " " + plural(days, "день", "дня", "дней"));
    $("expireText").style.color = days <= 3 ? "var(--bad)" : "";
  } else $("expireText").textContent = t("бессрочно");

  const vc = $("vpnCheck");
  vc.hidden = v.status !== "running" || !v.check;
  vc.className = "vcheck" + (v.check && v.check.startsWith("Проверяю") ? " wait" : v.checkOk ? "" : " bad");
  $("vpnCheckText").textContent = v.check || "";
  $("announce").hidden = !info.announce;
  $("announce").textContent = info.announce || "";
  $("subSupport").hidden = !info.supportUrl;
  $("subWeb").hidden = !info.webPage;

  for (const b of $("vpnMode").children) b.classList.toggle("sel", b.dataset.v === v.mode);
  const route = s.config.vpnRouteAll ? "all" : "blocked";
  for (const b of $("vpnRoute").children) b.classList.toggle("sel", b.dataset.v === route);
  $("routeHint").textContent = route === "all"
    ? t("Всё зарубежное — через VPN. Российские сайты, банки, Госуслуги, Яндекс и VK — напрямую.")
    : t("Через VPN — только сайты из базы блокировок и сервисы, закрытые для России. Остальное напрямую.");
  $("modeHint").textContent = v.mode === "tun"
    ? t("Перехватывает все программы и игры. В системе появляется виртуальный адаптер.")
    : t("Работают браузеры и большинство программ. Без виртуальных адаптеров.");

  // серверы
  const ul = $("servers");
  const list = [{ id: "auto", name: t("Авто"), meta: t("самый быстрый, переключается сам") }].concat(
    (v.servers || []).map((x) => ({ id: x.id, name: x.name, meta: x.protocol, ping: x })));
  const sig = list.map((x) => x.id).join("|") + "|" + LANG;
  if (ul.dataset.sig !== sig) {
    ul.innerHTML = "";
    for (const x of list) {
      const li = document.createElement("li");
      li.dataset.id = x.id;
      li.innerHTML = '<span class="srv"><span class="srv-name"></span><span class="srv-meta"></span></span>' +
        '<span class="srv-right"><span class="ping"></span>' + CHECK_SVG + "</span>";
      li.querySelector(".srv-name").textContent = x.name;
      li.querySelector(".srv-meta").textContent = x.meta;
      if (x.id !== "auto") li.querySelector(".srv-meta").classList.add("proto");
      li.addEventListener("click", () => guarded(() => api().SelectServer(x.id)));
      ul.appendChild(li);
    }
    ul.dataset.sig = sig;
  }
  const byId = Object.fromEntries((v.servers || []).map((x) => [x.id, x]));
  for (const li of ul.children) {
    li.classList.toggle("sel", li.dataset.id === v.selected);
    const p = li.querySelector(".ping");
    const sv = byId[li.dataset.id];
    p.textContent = sv ? pingText(sv) : "";
    p.className = "ping " + (sv ? pingClass(sv.ping) : "");
  }
}

// ---------------- обход блокировок (zapret) ----------------

function renderZapretView(s) {
  const z = s.zapret;
  const pk = s.pick || {};
  const pb = $("pickBtn"), ps = $("pickStatus");
  pb.textContent = t(pk.running ? "Остановить подбор" : "Подобрать автоматически");
  pb.className = "btn wide " + (pk.running ? "ghost-btn" : "primary");
  ps.hidden = !pk.running && !pk.result;
  ps.className = "hint pick-status" + (pk.running ? "" : pk.ok ? " ok" : " bad");
  ps.textContent = pk.running
    ? t("Пробую {x} из {y}: ", (pk.done || 0) + 1, pk.total || "…") + (pk.current || "") + "…" +
      (pk.found ? " · " + t("подошло: {x}", pk.found) : "")
    : pk.result || "";
  renderPickList(pk, z.strategy);
  for (const li of document.querySelectorAll(".services li[data-key]")) {
    const r = s.checks[li.dataset.key];
    const val = li.querySelector(".val");
    li.className = "";
    if (s.checking) { li.classList.add("wait"); val.textContent = t("проверка"); }
    else if (!r) val.textContent = t(z.status === "running" ? "не проверено" : "обход выключен");
    else if (r.ok) { li.classList.add("ok"); val.textContent = t("открывается · {x} мс", r.latencyMs); }
    else { li.classList.add("bad"); val.textContent = r.detail || t("не открывается"); }
  }
  $("recheck").disabled = s.checking || z.status !== "running";

  const ul = $("strategies");
  const names = z.strategies && z.strategies.length ? z.strategies : [z.strategy];
  const sig = names.join("|") + "|" + LANG;
  if (ul.dataset.list !== sig) {
    ul.innerHTML = "";
    for (const n of names) {
      const li = document.createElement("li");
      li.dataset.name = n;
      li.innerHTML = "<span></span>" + CHECK_SVG;
      li.firstChild.textContent = prettyName(n);
      li.addEventListener("click", () => { if (!(S && S.pick && S.pick.running)) guarded(() => api().SetStrategy(n)); });
      ul.appendChild(li);
    }
    ul.dataset.list = sig;
  }
  for (const li of ul.children) li.classList.toggle("sel", li.dataset.name === z.strategy);

  renderGames(s.games || [], s.config.disabledGames || [], s.config.enabledGames || []);
  for (const b of $("gameFilter").children) b.classList.toggle("sel", b.dataset.v === (z.gameFilter || "disabled"));
  $("autoUpdate").checked = !!s.config.autoUpdate;
  let ver = z.version || t("не установлен");
  if (z.latestVersion && z.version && z.latestVersion !== z.version) ver += " → " + z.latestVersion;
  $("zver").textContent = ver;
}

// ---------------- настройки приложения ----------------

function renderSettings(s) {
  // Быстрые переходы показывают то же, что строки модулей на главной.
  $("navZapret").textContent = $("zapretSub").textContent;
  $("navVpn").textContent = $("vpnSub").textContent;
  $("navTg").textContent = $("tgSub").textContent;
  applyTheme(s.config.theme || "system");
  const lang = s.config.lang === "en" ? "en" : "ru";
  if (lang !== LANG) setLang(lang, false);
  for (const b of $("lang").children) b.classList.toggle("sel", b.dataset.v === LANG);
  $("autostart").checked = !!s.autostart;
  $("appVer").textContent = "Починка " + (s.version || "");
}

// Список «Подошли»: рабочие стратегии из последнего подбора, лучшие сверху.
function renderPickList(pk, current) {
  const res = (pk.results || []).slice(0, 7);
  const ul = $("pickList");
  const has = res.length > 0;
  ul.hidden = $("pickTitle").hidden = $("allTitle").hidden = !has;
  if (!has) return;
  const sig = res.map((r) => r.name + r.latencyMs + r.discord + r.youtube).join("|") + LANG + !!pk.running;
  if (ul.dataset.sig !== sig) {
    ul.innerHTML = "";
    for (const r of res) {
      const li = document.createElement("li");
      li.dataset.name = r.name;
      li.innerHTML = '<span class="srv"><span class="srv-name"></span><span class="srv-meta"></span></span>' +
        '<span class="srv-right"><span class="ping"></span>' + CHECK_SVG + "</span>";
      li.querySelector(".srv-name").textContent = r.pretty;
      const full = r.discord && r.youtube;
      li.querySelector(".srv-meta").textContent = full ? t("Discord и YouTube") : r.discord ? t("только Discord") : t("только YouTube");
      const p = li.querySelector(".ping");
      p.textContent = t("{x} мс", r.latencyMs);
      p.className = "ping " + (full ? (r.latencyMs < 1500 ? "good" : "mid") : "bad");
      li.addEventListener("click", () => { if (!(S && S.pick && S.pick.running)) guarded(() => api().ApplyPicked(r.name)); });
      ul.appendChild(li);
    }
    ul.dataset.sig = sig;
  }
  for (const li of ul.children) li.classList.toggle("sel", li.dataset.name === current);
}

// Тема: "system" — по Windows, "light"/"dark" — принудительно.
function applyTheme(th) {
  const root = document.documentElement;
  if (th === "light" || th === "dark") root.dataset.theme = th;
  else delete root.dataset.theme;
  try { localStorage.setItem("theme", th); } catch (e) {}
  for (const b of $("theme").children) b.classList.toggle("sel", b.dataset.v === th);
}

// setLang переключает язык: разметка переводится сразу, остальное —
// повторной отрисовкой из последнего состояния.
function setLang(lang, save) {
  LANG = lang === "en" ? "en" : "ru";
  try { localStorage.setItem("lang", LANG); } catch (e) {}
  translateStatic();
  if (save) {
    // Иначе отрисовка из старого состояния (там ещё прежний язык) откатила бы выбор.
    if (S) S.config.lang = LANG;
    api().SetLanguage(LANG);
  }
  if (S) render(S);
}

// Список игровых групп с тумблером у каждой. Строим один раз, дальше
// только обновляем галочки — иначе переключатель дёргается под курсором.
function renderGames(games, disabled, enabled) {
  const box = $("games");
  const sig = games.map((g) => g.id).join("|");
  if (box.dataset.sig !== sig) {
    box.innerHTML = "";
    for (const g of games) {
      const row = document.createElement("label");
      row.className = "item";
      row.innerHTML = '<span class="two"><span></span><small></small></span>' +
        '<span class="switch"><input type="checkbox"><i></i></span>';
      row.querySelector(".two > span").textContent = g.name;
      row.querySelector("small").textContent = g.games;
      const cb = row.querySelector("input");
      cb.dataset.id = g.id;
      if (g.defaultOff) cb.dataset.defaultOff = "1";
      cb.addEventListener("change", () => guarded(() => api().SetGameEnabled(g.id, cb.checked)));
      box.appendChild(row);
    }
    box.dataset.sig = sig;
  }
  for (const cb of box.querySelectorAll("input")) {
    const id = cb.dataset.id;
    cb.checked = !disabled.includes(id) && (enabled.includes(id) || !cb.dataset.defaultOff);
  }
}

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
for (const b of $("theme").children) b.addEventListener("click", () => { applyTheme(b.dataset.v); api().SetTheme(b.dataset.v); });
for (const b of $("lang").children) b.addEventListener("click", () => setLang(b.dataset.v, true));
$("openLogs").addEventListener("click", () => api().OpenLogsFolder());
$("autostart").addEventListener("change", (e) => guarded(async () => {
  const r = await api().SetAutostart(e.target.checked);
  if (r) { notify(r); if (S) render(S); }
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

// ---------------- свои списки ----------------

const LISTS = {
  "zapret-include": { title: "Домены для обхода", hint: "По одному домену в строке. Поддомены включаются сами (yandex.ru = *.yandex.ru); «^» в начале — только сам домен. Шаблоны и regexp тут не работают — их понимает только VPN. Строки с # — комментарии." },
  "zapret-exclude": { title: "Исключения: домены", hint: "Сайты, которые обход не трогает. По одному домену в строке." },
  "zapret-ipexclude": { title: "Исключения: адреса", hint: "IP-адреса и подсети, которые обход не трогает: 1.2.3.4 или 1.2.3.0/24, по одному в строке." },
  "vpn-sites": { title: "Всегда через VPN", hint: "По одному в строке: домен (example.com — с поддоменами; можно вставить ссылку), шаблон (yandex.*, img*.site.ru), regexp:… или keyword:слово, IP или подсеть (1.2.3.0/24), программа (Discord.exe — имя как в Диспетчере задач). Эти правила важнее всех остальных." },
  "vpn-direct": { title: "Всегда напрямую", hint: "По одному в строке: домен, шаблон (yandex.*), regexp:…, keyword:…, IP или подсеть, программа (.exe). Всё это пойдёт мимо VPN, даже если сайт в базе блокировок. Если что-то есть в обоих списках — побеждает «напрямую»." },
};
let listFrom = "viewMain";
let listID = "";

async function openList(id) {
  const meta = LISTS[id];
  if (!meta) return;
  listID = id;
  listFrom = [...document.querySelectorAll(".view")].find((v) => !v.hidden)?.id || "viewMain";
  $("listTitle").textContent = t(meta.title);
  $("listHint").textContent = t(meta.hint);
  $("listStatus").textContent = "";
  $("listStatus").className = "list-status";
  $("listText").value = await api().GetUserList(id);
  show("viewList");
  $("listText").focus();
}

for (const b of document.querySelectorAll("[data-list]")) b.addEventListener("click", () => openList(b.dataset.list));

$("listSave").addEventListener("click", () => guarded(async () => {
  const st = $("listStatus");
  $("listSave").disabled = true;
  const r = await api().SaveUserList(listID, $("listText").value);
  $("listSave").disabled = false;
  if (r.error) {
    st.textContent = r.error;
    st.className = "list-status err";
    return;
  }
  st.textContent = t("Сохранено · записей: {x}", r.count);
  st.className = "list-status ok";
  $("listText").value = await api().GetUserList(listID); // как записалось после проверки
}));

// ---------------- диалоги ----------------
// Свои вместо alert()/confirm(): системные окна WebView2 выглядят чужими
// («wails.localhost says») и обрезают текст.

function ask(text, opts = {}) {
  const { ok = t("ОК"), cancel = t("Отмена"), danger = false } = opts;
  const dlg = $("dialog");
  $("dlgText").textContent = text;
  const okB = $("dlgOk"), noB = $("dlgCancel");
  okB.textContent = ok;
  okB.className = "btn " + (danger ? "danger-btn" : "primary");
  noB.hidden = cancel === null;
  noB.textContent = cancel || "";
  dlg.hidden = false;
  okB.focus();
  return new Promise((resolve) => {
    const done = (v) => {
      dlg.hidden = true;
      okB.onclick = noB.onclick = dlg.onclick = null;
      document.removeEventListener("keydown", key, true);
      resolve(v);
    };
    const key = (e) => {
      if (e.key === "Escape") { e.preventDefault(); done(false); }
      if (e.key === "Enter") { e.preventDefault(); done(true); }
    };
    okB.onclick = () => done(true);
    noB.onclick = () => done(false);
    dlg.onclick = (e) => { if (e.target === dlg) done(false); };
    document.addEventListener("keydown", key, true);
  });
}

const notify = (text) => ask(text, { cancel: null });
