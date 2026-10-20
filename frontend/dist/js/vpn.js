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
