// ---------------- горячие клавиши ----------------

const HK_CFG = { toggle: "hotkeyToggle", zapret: "hotkeyZapret", vpn: "hotkeyVpn" };

let hkCapturing = null; // data-hk строки, которая ждёт нажатие

// comboFromEvent - сочетание из события клавиатуры. По e.code (не зависит от
// раскладки), только буквы, цифры и F1–F24, обязательно с модификатором.
function comboFromEvent(e) {
  let key = null;
  if (/^Key[A-Z]$/.test(e.code)) key = e.code.slice(3);
  else if (/^Digit[0-9]$/.test(e.code)) key = e.code.slice(5);
  else if (/^F([1-9]|1[0-9]|2[0-4])$/.test(e.code)) key = e.code;
  else return null;
  const mods = [];
  if (e.ctrlKey) mods.push("Ctrl");
  if (e.altKey) mods.push("Alt");
  if (e.shiftKey) mods.push("Shift");
  if (e.metaKey) mods.push("Win");
  if (!mods.length) return null;
  return mods.concat(key).join("+");
}

function hkCombo(name) {
  return (S && S.config && S.config[HK_CFG[name]]) || "";
}

function renderHotkeys() {
  const nav = $("hotkeyNav");
  const combos = Object.keys(HK_CFG).map(hkCombo).filter(Boolean);
  nav.textContent = combos.length ? t("настроено: {x}", combos.join(", ")) : t("выключены");
  for (const row of $("hotkeyList").querySelectorAll("[data-hk]")) {
    const name = row.dataset.hk;
    const combo = hkCombo(name);
    const cap = hkCapturing === name;
    const box = row.querySelector(".hk-combo");
    row.classList.toggle("capturing", cap);
    box.textContent = cap ? t("нажмите сочетание…") : (combo || "-");
    box.classList.toggle("muted", !combo);
    row.querySelector("[data-set]").textContent = cap ? "…" : t("Изменить");
    row.querySelector("[data-set]").disabled = !!cap;
    row.querySelector("[data-clear]").hidden = !combo || cap;
  }
}

function hkStartCapture(name) {
  hkCapturing = name;
  renderHotkeys();
}

function hkEndCapture() {
  if (!hkCapturing) return;
  hkCapturing = null;
  renderHotkeys();
}

function hkKey(e) {
  if (!hkCapturing) return;
  e.preventDefault();
  e.stopPropagation();
  if (e.key === "Escape") { hkEndCapture(); return; }
  const combo = comboFromEvent(e);
  if (!combo) return;
  const name = hkCapturing;
  hkEndCapture();
  guarded(async () => {
    const r = await api().SetHotkey(name, combo);
    if (r) return notify(r);
    if (S) { S.config[HK_CFG[name]] = combo; render(S); }
  });
}

for (const row of document.querySelectorAll("#hotkeyList [data-hk]")) {
  const name = row.dataset.hk;
  row.querySelector("[data-set]").addEventListener("click", () => hkStartCapture(name));
  row.querySelector("[data-clear]").addEventListener("click", () => guarded(async () => {
    const r = await api().SetHotkey(name, "");
    if (r) return notify(r);
    if (S) { S.config[HK_CFG[name]] = ""; render(S); }
  }));
}
window.addEventListener("keydown", hkKey, true);
