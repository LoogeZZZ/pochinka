// Перевод интерфейса. Ключ — русская строка как она есть в разметке и в
// app.js, значение — английская. Чего нет в словаре, остаётся по-русски
// (например, сообщения бэкенда и журнал).
let LANG = "ru";
try { if (localStorage.getItem("lang") === "en") LANG = "en"; } catch (e) {}

const EN = {
  // шапка и общие
  "Настройки": "Settings",
  "Свернуть": "Minimise",
  "Скрыть в трей": "Hide to tray",
  "Подключить": "Connect",
  "Отключить": "Disconnect",
  "Нужны права администратора — перезапустите программу от имени администратора.": "Administrator rights required — restart the app as administrator.",
  "Работает другой VPN — «{x}». Выключите его: два VPN мешают друг другу, и сайты не грузятся.": "Another VPN is running — “{x}”. Turn it off: two VPNs interfere and sites won’t load.",

  // главная
  "Отключено": "Disconnected",
  "Подключение": "Connecting",
  "Подключено": "Connected",
  "Доступна версия {x}": "Version {x} is available",
  "Скачать": "Download",
  "Ошибка": "Error",
  "Нажмите, чтобы подключиться": "Tap to connect",
  "Секунду…": "One moment…",
  "Обход и VPN работают": "Unblocking and VPN are on",
  "VPN работает": "VPN is on",
  "Обход работает": "Unblocking is on",
  "Обход блокировок": "Unblocking",
  "Discord, YouTube, игры": "Discord, YouTube, games",
  "Добавить подписку": "Add subscription",
  "Прокси для Telegram Desktop": "Proxy for Telegram Desktop",
  "Запускаю…": "Starting…",
  "Проверяю Discord и YouTube…": "Checking Discord and YouTube…",
  "Discord и YouTube открываются": "Discord and YouTube work",
  "{x} не открывается — смените стратегию": "{x} doesn’t open — try another strategy",
  " и ": " and ",
  "Стратегия: {x}": "Strategy: {x}",
  "Выключен — сейчас работает VPN": "Off — VPN is active",
  "Выключен — сейчас работает обход": "Off — unblocking is active",
  "{x} · выключен": "{x} · off",
  "Подключаюсь…": "Connecting…",
  "Авто": "Auto",
  "Стандартная": "Default",

  // Telegram
  "Прокси для Telegram": "Telegram proxy",
  "Telegram Desktop не найден": "Telegram Desktop not found",
  "Подключён · соединений: {x}": "Connected · connections: {x}",
  "Прокси работает · Telegram не подключён": "Proxy is on · Telegram not connected",
  "Прокси готов — нажмите «Подключить Telegram»": "Proxy ready — press “Connect Telegram”",
  "выключен": "off",
  "подключён": "connected",
  "ожидает Telegram": "waiting for Telegram",
  "Telegram Desktop пойдёт через WebSocket — тем же путём, что и web.telegram.org. Работает без VPN и не трогает остальной трафик.":
    "Telegram Desktop will go through WebSocket — the same way as web.telegram.org. Works without VPN and doesn’t touch other traffic.",
  "Подключить Telegram": "Connect Telegram",
  "Telegram спросит «Подключить прокси?» — нажмите «Подключить».": "Telegram will ask “Enable proxy?” — press “Enable”.",
  "Установите Telegram Desktop с desktop.telegram.org, затем нажмите кнопку.": "Install Telegram Desktop from desktop.telegram.org, then press the button.",
  "Статистика": "Statistics",
  "Соединений сейчас": "Active connections",
  "{x} (всего {y})": "{x} (total {y})",
  "Через WebSocket / Cloudflare / TCP": "Via WebSocket / Cloudflare / TCP",
  "Трафик ↑ / ↓": "Traffic ↑ / ↓",
  "Прочее": "Other",
  "Скопировать ссылку прокси": "Copy proxy link",
  "для ручной настройки в Telegram на этом компьютере": "for manual setup in Telegram on this computer",
  "Скопировано": "Copied",

  // VPN
  "Обновить подписку": "Refresh subscription",
  "Добавьте подписку": "Add a subscription",
  "Ссылка из бота или личного кабинета. Подходят Remnawave, Marzban, 3x-ui, а также ссылки vless://, vmess://, trojan://, ss://, hysteria2://":
    "A link from your bot or dashboard. Remnawave, Marzban, 3x-ui and vless://, vmess://, trojan://, ss://, hysteria2:// links are supported",
  "Отмена": "Cancel",
  "Вставить": "Paste",
  "Добавить": "Add",
  "Загружаю…": "Loading…",
  "Вставьте ссылку на подписку": "Paste a subscription link",
  "Через VPN пойдут только заблокированные сайты. Яндекс, банки и Госуслуги — как обычно, напрямую.":
    "Only blocked sites go through VPN. Yandex, banks and Gosuslugi stay direct as usual.",
  "Подписка": "Subscription",
  "Проверить ещё раз": "Check again",
  "Поддержка": "Support",
  "Личный кабинет": "Dashboard",
  "Удалить": "Remove",
  "Режим": "Mode",
  "Системный прокси": "System proxy",
  "TUN — все программы": "TUN — all apps",
  "Серверы": "Servers",
  "Проверить пинг": "Check ping",
  "Меряю…": "Measuring…",
  "Через VPN": "Through VPN",
  "Только заблокированное": "Blocked only",
  "Всё, кроме российского": "All but Russian",
  "Свои сайты через VPN": "My sites through VPN",
  "всё остальное — по базе блокировок": "everything else follows the block list",
  "подключено": "connected",
  "подключаюсь": "connecting",
  "ошибка": "error",
  "выключено": "off",
  "{x} из {y}": "{x} of {y}",
  "{x} · безлимит": "{x} · unlimited",
  "Безлимит": "Unlimited",
  "истекла": "expired",
  "до {x} · {y}": "until {x} · {y}",
  "бессрочно": "no expiry",
  "Всё зарубежное — через VPN. Российские сайты, банки, Госуслуги, Яндекс и VK — напрямую.":
    "Everything foreign goes through VPN. Russian sites, banks, Gosuslugi, Yandex and VK stay direct.",
  "Через VPN — только сайты из базы блокировок и сервисы, закрытые для России. Остальное напрямую.":
    "Only sites from the block list and services closed to Russia go through VPN. The rest is direct.",
  "Перехватывает все программы и игры. В системе появляется виртуальный адаптер.":
    "Captures all apps and games. A virtual network adapter appears in the system.",
  "Работают браузеры и большинство программ. Без виртуальных адаптеров.":
    "Works for browsers and most apps. No virtual adapters.",
  "самый быстрый, переключается сам": "fastest, switches automatically",
  "не отвечает": "no response",
  "{x} мс": "{x} ms",
  "серверов: {x}": "servers: {x}",
  "Удалить подписку {x}? {y}": "Remove subscription {x}? {y}",
  "VPN переключится на другую подписку.": "VPN will switch to another subscription.",
  "VPN отключится.": "VPN will disconnect.",
  "Устройство": "Device",
  "Отправлять HWID устройства": "Send device HWID",
  "нужно панелям с лимитом устройств (Remnawave)": "required by panels with a device limit (Remnawave)",
  "HWID этого компьютера: {x}": "This computer's HWID: {x}",
  "Панель не узнает это устройство. Подписка с лимитом устройств может не загрузиться.": "The panel won't recognise this device. A subscription with a device limit may fail to load.",

  // обход
  "Проверка": "Check",
  "Проверить снова": "Check again",
  "проверка": "checking",
  "не проверено": "not checked",
  "обход выключен": "unblocking is off",
  "открывается · {x} мс": "works · {x} ms",
  "не открывается": "doesn’t open",
  "Стратегия": "Strategy",
  "Если сервис не открывается — выберите другую. Применяется сразу.": "If a service doesn’t open, pick another one. Applies immediately.",
  "Игры": "Games",
  "Порты для игр — включайте, если игра всё равно не подключается.": "Game ports — turn on if a game still can’t connect.",
  "Выкл": "Off",
  "Обновления": "Updates",
  "Обновлять zapret автоматически": "Update zapret automatically",
  "Проверить": "Check",
  "Проверяю…": "Checking…",
  "не установлен": "not installed",
  "Свои домены для обхода": "My domains to unblock",
  "Свои списки": "My lists",
  "Домены для обхода": "Domains to unblock",
  "сайты, которые обход тоже будет чинить": "sites the unblocker will also fix",
  "Исключения: домены": "Exclusions: domains",
  "сайты, которые обход не трогает": "sites the unblocker leaves alone",
  "Исключения: адреса": "Exclusions: addresses",
  "IP и подсети, которые обход не трогает": "IPs and subnets the unblocker leaves alone",
  "Сохранить": "Save",
  "Сохранено · записей: {x}": "Saved · entries: {x}",
  "По одному домену в строке. Поддомены включаются сами; «^» в начале — только сам домен. Строки с # — комментарии.": "One domain per line. Subdomains are included; a leading “^” means the domain only. Lines starting with # are comments.",
  "Сайты, которые обход не трогает. По одному домену в строке.": "Sites the unblocker leaves alone. One domain per line.",
  "IP-адреса и подсети, которые обход не трогает: 1.2.3.4 или 1.2.3.0/24, по одному в строке.": "IP addresses and subnets the unblocker leaves alone: 1.2.3.4 or 1.2.3.0/24, one per line.",
  "Эти сайты всегда открываются через VPN. По одному домену в строке; можно вставлять ссылки — домен возьмётся сам.": "These sites always go through VPN. One domain per line; you can paste links — the domain is taken automatically.",

  // настройки
  "Разделы": "Sections",
  "Приложение": "App",
  "Тема": "Theme",
  "Как в системе": "System",
  "Светлая": "Light",
  "Тёмная": "Dark",
  "Язык": "Language",
  "Журнал": "Log",
  "Лицензии": "Licenses",
  "Папка": "Folder",
  "Выйти из программы": "Quit the app",
  "Запускать вместе с Windows": "Start with Windows",
  "сразу в трей, без окна": "straight to the tray, no window",
};

// t — перевод строки с подстановкой {x}, {y}.
function t(s, x, y) {
  let r = LANG === "en" && EN[s] !== undefined ? EN[s] : s;
  if (x !== undefined) r = r.replace("{x}", x);
  if (y !== undefined) r = r.replace("{y}", y);
  return r;
}

// plural — «1 день / 2 дня / 5 дней», по-английски day/days.
function plural(n, one, few, many) {
  if (LANG === "en") return n === 1 ? "day" : "days";
  const m10 = n % 10, m100 = n % 100;
  if (m10 === 1 && m100 !== 11) return one;
  if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return few;
  return many;
}

// translateStatic переводит текст, вписанный в разметку. Исходник каждого
// узла запоминается, поэтому переключать язык можно туда и обратно.
const ATTRS = ["title", "placeholder", "aria-label"];
function translateStatic() {
  const w = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  for (let n = w.nextNode(); n; n = w.nextNode()) {
    if (n.__ru === undefined) {
      const k = n.nodeValue.trim();
      if (!k || EN[k] === undefined) continue;
      n.__ru = n.nodeValue;
    }
    const k = n.__ru.trim();
    n.nodeValue = LANG === "en" ? n.__ru.replace(k, EN[k]) : n.__ru;
  }
  for (const el of document.querySelectorAll(ATTRS.map((a) => "[" + a + "]").join(","))) {
    for (const a of ATTRS) {
      if (!el.hasAttribute(a)) continue;
      const key = "ru" + a.replace("-", "");
      if (el.dataset[key] === undefined) {
        if (EN[el.getAttribute(a)] === undefined) continue;
        el.dataset[key] = el.getAttribute(a);
      }
      el.setAttribute(a, t(el.dataset[key]));
    }
  }
  document.documentElement.lang = LANG;
}
