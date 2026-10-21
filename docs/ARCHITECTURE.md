# Как устроена Починка

Одно окно на Wails (Go + WebView2) и три независимых модуля: обход блокировок (zapret), VPN по подписке (xray-core) и прокси для Telegram. Программа работает с правами администратора; данные — в `C:\ProgramData\Pochinka` (только для администраторов), сама программа — в `C:\Program Files\Pochinka`.

## Дерево проекта

```
main.go                 запуск: права, окружение, установка, окно Wails
internal/
  app/                  всё, что вызывает окно: состояние, переключатели, трей,
                        темы, подбор стратегии, отчёт, обновление, удаление
  install/              Program Files, задачи планировщика (запуск без UAC,
                        автозапуск), замена exe, удаление после выхода
  selfupdate/           проверка версии на GitHub, скачивание, подпись Ed25519
  zapret/               установка и запуск zapret, стратегии, игровые списки
  vpn/                  подписки, конфиг xray, системный прокси, TUN, HWID
  tgproxy/              прокси для Telegram Desktop (порт tg-ws-proxy)
  health/               проверка «открывается ли»: YouTube, Discord и т.д., DoH
  securedns/            защищённый DNS Windows (включается только по кнопке)
  hostsfile/            свой блок в hosts: записи Discord и Telegram из zapret
  userlists/            свои списки доменов, IP и программ
  config/ paths/ applog/ winutil/ assets/   настройки, папки и права, журнал,
                        WinAPI, вшитые файлы (иконки, geo-базы, wintun)
frontend/dist/          интерфейс без сборщиков
  index.html            разметка всех экранов
  css/style.css         стили и темы system/light/dark
  fonts/                шрифт флагов
  js/                   i18n → themes → core → status → vpn → zapret →
                        settings → lists → dialogs → main (порядок важен)
tools/                  signer (подпись релизов), genlicenses.sh, genicon, geotags
third_party/            тексты лицензий зависимостей
build/                  иконка и манифест Windows для Wails
docs/                   эта страница и RELEASING.md
```

## Связь окна и Go

Методы `*app.App` доступны в окне как `window.go.app.App.<Метод>()`. Состояние отправляется событием `state` целиком (`UIState`), окно отрисовывает его функцией `render`. Строки журнала приходят событием `log`.

## Скрипты интерфейса

Обычные `<script>` без модулей: функции и переменные общие. Файлы только объявляют функции, вешают обработчики и запускают начальную загрузку в `main.js`, поэтому он подключается последним.
