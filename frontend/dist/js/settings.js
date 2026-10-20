// ---------------- настройки приложения ----------------

function renderSettings(s) {
  // Быстрые переходы показывают то же, что строки модулей на главной.
  $("navZapret").textContent = $("zapretSub").textContent;
  $("navVpn").textContent = $("vpnSub").textContent;
  $("navTg").textContent = $("tgSub").textContent;
  const sd = s.secureDns || {};
  for (const b of $("sdns").children) b.classList.toggle("sel", b.dataset.v === (sd.provider || ""));
  $("sdnsHint").className = "hint" + (sd.error ? " err" : "");
  $("sdnsHint").textContent = sd.error ? sd.error
    : t(sd.doh
      ? "DNS-запросы шифруются (DoH) — провайдер не сможет подменить адрес сайта. Прежние настройки сетевых карт вернутся при выключении."
      : "В этой версии Windows шифрование DNS недоступно: будут только серверы выбранного сервиса. Прежние настройки вернутся при выключении.");
  CUSTOM = s.config.customTheme || null;
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

// Тема: system/light/dark — из style.css; готовые и своя — палитры (themes.js).
let CUSTOM = null; // своя палитра из настроек

function applyTheme(th) {
  const p = paletteFor(th, CUSTOM);
  applyPalette(th, p);
  try {
    localStorage.setItem("theme", th);
    if (p) localStorage.setItem("themeVars", JSON.stringify({ scheme: p.scheme, vars: themeVars(p) }));
    else localStorage.removeItem("themeVars");
  } catch (e) {}
  renderThemeGrid(th);
}

// Сетка тем: плитка-превью для каждой.
const BUILTIN = [
  { id: "system", name: "Как в системе" },
  { id: "light", name: "Светлая", p: BASE_LIGHT },
  { id: "dark", name: "Тёмная", p: BASE_DARK },
];
function renderThemeGrid(cur) {
  const box = $("theme");
  const items = BUILTIN.concat(Object.entries(THEMES).map(([id, p]) => ({ id, name: p.name, p })));
  if (validPalette(CUSTOM)) items.push({ id: "custom", name: "Своя", p: CUSTOM });
  const sig = items.map((x) => x.id).join("|") + LANG + JSON.stringify(CUSTOM);
  if (box.dataset.sig !== sig) {
    box.innerHTML = "";
    for (const it of items) {
      const b = document.createElement("button");
      b.className = "theme-tile";
      b.dataset.v = it.id;
      const sw = document.createElement("span");
      sw.className = "tt-sw";
      if (it.p) {
        sw.style.background = it.p.bg;
        sw.innerHTML = "<i></i><i></i><i></i>";
        sw.children[0].style.background = it.p.surface;
        sw.children[1].style.background = it.p.accent;
        sw.children[2].style.background = it.p.on;
      } else {
        sw.classList.add("tt-system");
      }
      const nm = document.createElement("span");
      nm.className = "tt-name";
      nm.textContent = t(it.name);
      b.append(sw, nm);
      b.addEventListener("click", () => { applyTheme(it.id); api().SetTheme(it.id); });
      box.appendChild(b);
    }
    box.dataset.sig = sig;
  }
  for (const b of box.children) b.classList.toggle("sel", b.dataset.v === cur);
}

// ---- редактор своей темы ----
const COLOR_LABELS = {
  bg: "Фон", surface: "Карточки", text: "Текст", muted: "Второстепенный текст",
  accent: "Акцент", on: "Успех", bad: "Ошибка", warn: "Предупреждение",
};
let draft = null;

function openThemeEditor() {
  const cur = (S && S.config && S.config.theme) || "system";
  const base = paletteFor(cur, CUSTOM) ||
    (document.documentElement.dataset.theme === "light" || (cur === "system" && !matchMedia("(prefers-color-scheme: dark)").matches) ? BASE_LIGHT : BASE_DARK);
  draft = { ...base };
  delete draft.name;
  renderThemeEditor();
  show("viewTheme");
}

function renderThemeEditor() {
  for (const b of $("themeScheme").children) b.classList.toggle("sel", b.dataset.v === draft.scheme);
  const from = $("themeFrom");
  if (!from.dataset.built) {
    const src = [["Светлая", BASE_LIGHT], ["Тёмная", BASE_DARK]].concat(Object.values(THEMES).map((p) => [p.name, p]));
    for (const [name, p] of src) {
      const c = document.createElement("button");
      c.className = "chip";
      c.textContent = t(name);
      c.addEventListener("click", () => { draft = { ...p }; delete draft.name; renderThemeEditor(); });
      from.appendChild(c);
    }
    from.dataset.built = "1";
  }
  const list = $("themeColors");
  if (!list.dataset.built) {
    for (const k of THEME_KEYS) {
      const row = document.createElement("label");
      row.className = "item color-row";
      row.innerHTML = '<span></span><span class="color-val"><code></code><input type="color"></span>';
      row.firstChild.textContent = t(COLOR_LABELS[k]);
      const inp = row.querySelector("input");
      inp.dataset.k = k;
      inp.addEventListener("input", () => { draft[k] = inp.value.toLowerCase(); renderThemeEditor(); });
      list.appendChild(row);
    }
    list.dataset.built = "1";
  }
  for (const inp of list.querySelectorAll("input")) {
    inp.value = draft[inp.dataset.k];
    inp.parentElement.querySelector("code").textContent = draft[inp.dataset.k];
  }
  // Превью — в цветах черновика, не трогая само окно.
  const pv = $("themePreview"), vars = themeVars(draft);
  for (const k in vars) pv.style.setProperty(k, vars[k]);
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
