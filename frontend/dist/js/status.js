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
