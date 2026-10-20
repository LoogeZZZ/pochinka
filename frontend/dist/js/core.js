// Связь с Go: window.go.app.App.* — методы, window.runtime.* — окно и события.
// Перевод (t, plural, translateStatic, LANG) — в i18n.js.
const api = () => window.go.app.App;
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
