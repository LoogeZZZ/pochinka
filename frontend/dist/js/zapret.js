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
  const dnsBad = Object.entries(s.checks || {}).filter(([, r]) => r && r.dnsBlocked).map(([k]) => (k === "youtube" ? "YouTube" : k === "discord" ? "Discord" : k));
  const sdOn = !!(s.secureDns && s.secureDns.provider);
  $("dnsNote").hidden = !dnsBad.length;
  $("dnsNoteText").textContent = dnsBad.length
    ? t("DNS провайдера не отдаёт адрес {x}: в браузере сайт может не открыться, даже если обход работает.", dnsBad.join(t(" и ")))
    : "";
  $("dnsFix").hidden = sdOn;

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
