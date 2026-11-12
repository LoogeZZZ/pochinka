// ---------------- обход блокировок (zapret) ----------------

// Проверка доступности: встроенные сервисы + свои сайты из настроек.
const CHECK_KEYS = ["discord", "youtube", "twitch"];
const CHECK_NAMES = { discord: "Discord", youtube: "YouTube", twitch: "Twitch" };

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

  // Список проверок: встроенные и свои (одним блоком, строка ввода - в конце
  // того же списка); свои можно убрать крестиком.
  const keys = CHECK_KEYS.concat(s.config.checkSites || []);
  const sul = $("services");
  const sig = keys.join("|") + "|" + LANG;
  if (sul.dataset.sig !== sig) {
    for (const old of sul.querySelectorAll("li[data-key]")) old.remove();
    for (const k of keys) {
      const li = document.createElement("li");
      li.dataset.key = k;
      const custom = !CHECK_KEYS.includes(k);
      li.innerHTML = "<span></span>" +
        '<span class="val">-</span>' +
        (custom ? '<button class="link rm" title="Убрать">×</button>' : "");
      li.firstChild.textContent = CHECK_NAMES[k] || k;
      if (custom) li.querySelector(".rm").addEventListener("click", () => guarded(async () => {
        const sites = (S.config.checkSites || []).filter((x) => x !== k);
        const r = await api().SetCheckSites(sites);
        if (r) notify(r);
      }));
      sul.insertBefore(li, $("checkSiteRow"));
    }
    sul.dataset.sig = sig;
  }
  for (const li of sul.querySelectorAll("li[data-key]")) {
    const r = s.checks[li.dataset.key];
    const val = li.querySelector(".val");
    li.className = "";
    if (s.checking) { li.classList.add("wait"); val.textContent = t("проверка"); }
    else if (!r) val.textContent = t(z.status === "running" ? "не проверено" : "обход выключен");
    else if (r.ok) { li.classList.add("ok"); val.textContent = t("открывается · {x} мс", r.latencyMs); }
    else { li.classList.add("bad"); val.textContent = r.detail || t("не открывается"); }
  }
  $("recheck").disabled = s.checking || z.status !== "running";
  const dnsBad = Object.entries(s.checks || {}).filter(([, r]) => r && r.dnsBlocked).map(([k]) => (CHECK_NAMES[k] || k));
  const sdOn = !!(s.secureDns && s.secureDns.provider);
  $("dnsNote").hidden = !dnsBad.length;
  $("dnsNoteText").textContent = dnsBad.length
    ? t("DNS провайдера не отдаёт адрес {x}: в браузере сайт может не открыться, даже если обход работает.", dnsBad.join(t(" и ")))
    : "";
  $("dnsFix").hidden = sdOn;

  const ul = $("strategies");
  const names = z.strategies && z.strategies.length ? z.strategies : [z.strategy];
  const ssig = names.join("|") + "|" + LANG;
  if (ul.dataset.list !== ssig) {
    ul.innerHTML = "";
    for (const n of names) {
      const li = document.createElement("li");
      li.dataset.name = n;
      li.innerHTML = "<span></span>" + CHECK_SVG;
      li.firstChild.textContent = prettyName(n);
      li.addEventListener("click", () => { if (!(S && S.pick && S.pick.running)) guarded(() => api().SetStrategy(n)); });
      ul.appendChild(li);
    }
    ul.dataset.list = ssig;
  }
  for (const li of ul.children) li.classList.toggle("sel", li.dataset.name === z.strategy);

  renderGames(s.games || [], s.config.disabledGames || [], s.config.enabledGames || []);
  for (const b of $("gameFilter").children) b.classList.toggle("sel", b.dataset.v === (z.gameFilter || "disabled"));
  for (const b of $("ipset").children) b.classList.toggle("sel", b.dataset.v === (z.ipset || ""));
  $("ipsetDate").textContent = z.ipsetUpdated
    ? t("обновлён {x}", new Date(z.ipsetUpdated).toLocaleDateString(LANG === "en" ? "en-GB" : "ru-RU", { day: "numeric", month: "long" }))
    : t("сейчас - список из zapret");
  $("hostsBlock").checked = !!s.config.hostsBlock;
  $("autoUpdate").checked = !!s.config.autoUpdate;
  let ver = z.version || t("не установлен");
  if (z.latestVersion && z.version && z.latestVersion !== z.version) ver += " → " + z.latestVersion;
  $("zver").textContent = ver;
}
