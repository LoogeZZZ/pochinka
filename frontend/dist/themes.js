// Темы оформления. «system», «light», «dark» описаны в style.css; готовые
// темы и своя задаются набором из 8 цветов — линии, подсветка и рамки
// вычисляются из текста, чтобы любая палитра выглядела цельно.
const THEME_KEYS = ["bg", "surface", "text", "muted", "accent", "on", "bad", "warn"];

const THEMES = {
  // Официальные палитры тем (цвета взяты из их спецификаций).
  dracula: { name: "Dracula", scheme: "dark",
    bg: "#21222c", surface: "#282a36", text: "#f8f8f2", muted: "#6272a4",
    accent: "#bd93f9", on: "#50fa7b", bad: "#ff5555", warn: "#ffb86c" },
  nord: { name: "Nord", scheme: "dark",
    bg: "#2e3440", surface: "#3b4252", text: "#eceff4", muted: "#a3abbd",
    accent: "#88c0d0", on: "#a3be8c", bad: "#bf616a", warn: "#ebcb8b" },
  ubuntu: { name: "Ubuntu", scheme: "dark",
    bg: "#2c001e", surface: "#3d0f2e", text: "#ffffff", muted: "#c0a6b5",
    accent: "#e95420", on: "#73d216", bad: "#ef2929", warn: "#f99b11" },
  github: { name: "GitHub", scheme: "light",
    bg: "#f6f8fa", surface: "#ffffff", text: "#1f2328", muted: "#59636e",
    accent: "#0969da", on: "#1a7f37", bad: "#d1242f", warn: "#9a6700" },
  iceberg: { name: "Iceberg", scheme: "dark",
    bg: "#161821", surface: "#1e2132", text: "#c6c8d1", muted: "#6b7089",
    accent: "#84a0c6", on: "#b4be82", bad: "#e27878", warn: "#e2a478" },
  monokai: { name: "Monokai", scheme: "dark",
    bg: "#1e1f1c", surface: "#272822", text: "#f8f8f2", muted: "#90908a",
    accent: "#66d9ef", on: "#a6e22e", bad: "#f92672", warn: "#fd971f" },
};

// Встроенные светлая и тёмная — как опорные палитры для своей темы.
const BASE_LIGHT = { scheme: "light", bg: "#f4f5f7", surface: "#ffffff", text: "#0f1115", muted: "#6c717a",
  accent: "#4f46e5", on: "#15983f", bad: "#d42020", warn: "#c26a04" };
const BASE_DARK = { scheme: "dark", bg: "#111214", surface: "#18191c", text: "#ececee", muted: "#7d828c",
  accent: "#8b8cf8", on: "#34d17a", bad: "#f26b6b", warn: "#f0b44c" };

const HEX = /^#[0-9a-f]{6}$/i;

// validPalette — только #rrggbb и известные ключи: значения идут в CSS.
function validPalette(p) {
  if (!p || (p.scheme !== "light" && p.scheme !== "dark")) return false;
  return THEME_KEYS.every((k) => HEX.test(p[k] || ""));
}

// themeVars — CSS-переменные палитры, включая производные.
function themeVars(p) {
  const dark = p.scheme === "dark";
  return {
    "--bg": p.bg, "--surface": p.surface, "--text": p.text, "--muted": p.muted,
    "--accent": p.accent, "--on": p.on, "--bad": p.bad, "--warn": p.warn,
    "--line": `color-mix(in srgb, ${p.text} ${dark ? 8 : 9}%, transparent)`,
    "--hover": `color-mix(in srgb, ${p.text} ${dark ? 6 : 5}%, transparent)`,
    "--ring": `color-mix(in srgb, ${p.text} ${dark ? 12 : 13}%, transparent)`,
  };
}

// paletteFor — палитра темы по имени (custom — из сохранённой своей).
function paletteFor(name, custom) {
  if (THEMES[name]) return THEMES[name];
  if (name === "custom" && validPalette(custom)) return custom;
  return null;
}

// applyPalette ставит тему на <html>. null — тема из style.css (system/light/dark).
function applyPalette(name, p) {
  const root = document.documentElement;
  for (const k of [...THEME_KEYS.map((x) => "--" + x), "--line", "--hover", "--ring"]) root.style.removeProperty(k);
  if (!p) {
    if (name === "light" || name === "dark") root.dataset.theme = name;
    else delete root.dataset.theme;
    return;
  }
  root.dataset.theme = p.scheme; // color-scheme и подложка — от основы
  const vars = themeVars(p);
  for (const k in vars) root.style.setProperty(k, vars[k]);
}
