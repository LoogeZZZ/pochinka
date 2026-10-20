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
