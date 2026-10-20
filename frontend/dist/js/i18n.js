// Перевод интерфейса. Ключ — русская строка как она есть в разметке и в
// js/*.js, значение — английская. Чего нет в словаре, остаётся по-русски
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
  "Обновить": "Update",
  "Что нового": "What's new",
  "Ещё раз": "Retry",
  "Скачать вручную": "Download manually",
  "Обновляю до {x}… {y}%": "Updating to {x}… {y}%",
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
  "ОК": "OK",
  "Продолжить": "Continue",
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
  "Подобрать автоматически": "Pick automatically",
  "Подошли — выберите любую": "These work — pick any",
  "Все стратегии": "All strategies",
  "подошло: {x}": "working: {x}",
  "Discord и YouTube": "Discord and YouTube",
  "только Discord": "Discord only",
  "только YouTube": "YouTube only",
  "Остановить подбор": "Stop picking",
  "Пробую {x} из {y}: ": "Trying {x} of {y}: ",
  "Для подбора нужен обход — VPN будет выключен. Продолжить?": "Picking needs unblocking — VPN will be turned off. Continue?",
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
  "Всегда через VPN": "Always through VPN",
  "домены, IP и программы — поверх всех правил": "domains, IPs and apps — above all rules",
  "Всегда напрямую": "Always direct",
  "домены, IP и программы мимо VPN": "domains, IPs and apps bypassing VPN",
  "По одному в строке: домен (example.com — с поддоменами; можно вставить ссылку), шаблон (yandex.*, img*.site.ru), regexp:… или keyword:слово, IP или подсеть (1.2.3.0/24), программа (Discord.exe — имя как в Диспетчере задач). Эти правила важнее всех остальных.": "One per line: a domain (example.com — with subdomains; links work too), a pattern (yandex.*, img*.site.ru), regexp:… or keyword:word, an IP or subnet (1.2.3.0/24), an app (Discord.exe — the name from Task Manager). These rules beat all others.",
  "По одному в строке: домен, шаблон (yandex.*), regexp:…, keyword:…, IP или подсеть, программа (.exe). Всё это пойдёт мимо VPN, даже если сайт в базе блокировок. Если что-то есть в обоих списках — побеждает «напрямую».": "One per line: a domain, a pattern (yandex.*), regexp:…, keyword:…, an IP or subnet, an app (.exe). All of it bypasses VPN, even if the site is on the block list. If something is in both lists, “direct” wins.",
  "Домены для обхода": "Domains to unblock",
  "сайты, которые обход тоже будет чинить": "sites the unblocker will also fix",
  "Исключения: домены": "Exclusions: domains",
  "сайты, которые обход не трогает": "sites the unblocker leaves alone",
  "Исключения: адреса": "Exclusions: addresses",
  "IP и подсети, которые обход не трогает": "IPs and subnets the unblocker leaves alone",
  "Сохранить": "Save",
  "Сохранено · записей: {x}": "Saved · entries: {x}",
  "По одному домену в строке. Поддомены включаются сами (yandex.ru = *.yandex.ru); «^» в начале — только сам домен. Шаблоны и regexp тут не работают — их понимает только VPN. Строки с # — комментарии.": "One domain per line. Subdomains are included (yandex.ru = *.yandex.ru); a leading “^” means the domain only. Patterns and regexp don't work here — only VPN understands them. Lines starting with # are comments.",
  "Сайты, которые обход не трогает. По одному домену в строке.": "Sites the unblocker leaves alone. One domain per line.",
  "IP-адреса и подсети, которые обход не трогает: 1.2.3.4 или 1.2.3.0/24, по одному в строке.": "IP addresses and subnets the unblocker leaves alone: 1.2.3.4 or 1.2.3.0/24, one per line.",
  "Эти сайты всегда открываются через VPN. По одному домену в строке; можно вставлять ссылки — домен возьмётся сам.": "These sites always go through VPN. One domain per line; you can paste links — the domain is taken automatically.",

  // настройки
  "Разделы": "Sections",
  "Приложение": "App",
  "Тема": "Theme",
  "Защищённый DNS": "Secure DNS",
  "Включить защищённый DNS": "Turn on secure DNS",
  "DNS-запросы шифруются (DoH) — провайдер не сможет подменить адрес сайта. Прежние настройки сетевых карт вернутся при выключении.": "DNS queries are encrypted (DoH) — your ISP can't spoof site addresses. Previous adapter settings come back when you turn it off.",
  "В этой версии Windows шифрование DNS недоступно: будут только серверы выбранного сервиса. Прежние настройки вернутся при выключении.": "This Windows version can't encrypt DNS: only the chosen service's servers will be used. Previous settings come back when you turn it off.",
  "DNS провайдера не отдаёт адрес {x}: в браузере сайт может не открыться, даже если обход работает.": "Your ISP's DNS doesn't return the address of {x}: the site may not open in the browser even though unblocking works.",
  "Как в системе": "System",
  "Светлая": "Light",
  "Тёмная": "Dark",
  "Своя": "Custom",
  "Своя тема": "Custom theme",
  "подобрать цвета самому": "pick your own colours",
  "Основа": "Base",
  "Взять цвета из": "Start from",
  "Цвета": "Colours",
  "Применить": "Apply",
  "Фон": "Background",
  "Карточки": "Cards",
  "Текст": "Text",
  "Второстепенный текст": "Secondary text",
  "Акцент": "Accent",
  "Успех": "Success",
  "Предупреждение": "Warning",
  "Кнопка": "Button",
  "открывается": "works",
  "внимание": "warning",
  "Язык": "Language",
  "Журнал": "Log",
  "Скопировать отчёт": "Copy report",
  "для вопроса в поддержку: состояние и журнал, без подписок и ключей": "for a support question: status and log, without subscriptions or keys",
  "Лицензии": "Licenses",
  "Проверить обновления": "Check for updates",
  "Позже": "Later",
  "Не удалось проверить обновления: {x}": "Couldn't check for updates: {x}",
  "У вас последняя версия: {x}": "You have the latest version: {x}",
  "Доступна версия {x}. Обновить сейчас?": "Version {x} is available. Update now?",
  "Папка": "Folder",
  "Выйти из программы": "Quit the app",
  "Удалить Починку": "Uninstall Pochinka",
  "убрать программу, её настройки и всё, что она меняла": "remove the app, its settings and everything it changed",
  "Удалить Починку? Будут выключены обход, VPN и прокси для Telegram, возвращены настройки прокси Windows и удалены: копия программы, подписки, настройки и журналы. Скачанный вами exe останется — его можно удалить вручную.": "Uninstall Pochinka? Unblocking, VPN and the Telegram proxy will be turned off, Windows proxy settings restored, and the app copy, subscriptions, settings and logs deleted. The exe you downloaded stays — you can delete it manually.",
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
