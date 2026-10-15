# Сторонние компоненты

Починка распространяется под лицензией MIT (см. `LICENSE`). Ниже перечислено всё чужое, что вшито в exe или использовано в коде. Полные тексты лицензий лежат в `third_party/THIRD_PARTY_LICENSES.txt`; они же вшиты в программу (Настройки → Лицензии). Файл пересобирается командой `bash tools/genlicenses.sh`.

## Основа

| Компонент | Как используется | Лицензия |
|---|---|---|
| [Flowseal/tg-ws-proxy](https://github.com/Flowseal/tg-ws-proxy) | портирован на Go: `internal/tgproxy` | MIT, © 2026 Flowseal |
| [Flowseal/zapret-discord-youtube](https://github.com/Flowseal/zapret-discord-youtube) | скачивается с GitHub при запуске; в коде разбор формата стратегий и игровые списки по мотивам issues | MIT, © 2016–2026 bol-van, © 2024–2026 Flowseal |
| [bol-van/zapret](https://github.com/bol-van/zapret) (winws) | входит в релиз zapret-discord-youtube, скачивается при запуске | MIT |
| [WinDivert](https://github.com/basil00/WinDivert) | входит в релиз zapret-discord-youtube, скачивается при запуске, Починкой не распространяется | LGPL-3.0 или GPL-2.0 |
| [XTLS/Xray-core](https://github.com/XTLS/Xray-core) | VPN-движок, вшит как библиотека без изменений | MPL-2.0 |

## Вшито в exe

| Компонент | Что это | Лицензия |
|---|---|---|
| [Wintun](https://www.wintun.net/) `wintun.dll` | драйвер TUN, бинарник без изменений, используется только через API `wintun.h` | Wintun Prebuilt Binaries License (WireGuard LLC), распространение вместе с программой разрешено |
| [runetfreedom/russia-blocked-geoip](https://github.com/runetfreedom/russia-blocked-geoip), [russia-blocked-geosite](https://github.com/runetfreedom/russia-blocked-geosite) | базы `geoip-ru-only.dat`, `geosite-ru-only.dat`: данные, без изменений, обновляются с GitHub | GPL-3.0 (исходники по ссылкам) |
| [Twemoji Country Flags](https://github.com/talkjs/country-flag-emoji-polyfill) | шрифт флагов стран | MIT (код), CC-BY-4.0 (графика [Twemoji](https://github.com/jdecked/twemoji), © Twitter/X и участники) |
| [Wails](https://github.com/wailsapp/wails) | окно и WebView2 | MIT |
| [energye/systray](https://github.com/energye/systray) | иконка в трее | Apache-2.0 |

## Go-модули в сборке

| Модуль | Версия | Лицензия |
|---|---|---|
| github.com/andybalholm/brotli | v1.0.6 | MIT |
| github.com/apernet/quic-go | v0.61.1-0.20260806010916-184d081eef3e | MIT |
| github.com/cloudflare/circl | v1.6.5 | BSD |
| github.com/energye/systray | v1.0.3 | Apache-2.0 |
| github.com/ghodss/yaml | v1.0.1-0.20220118164431-d8423dcdf344 | BSD |
| github.com/google/btree | v1.1.2 | Apache-2.0 |
| github.com/google/gopacket | v1.1.19 | BSD |
| github.com/google/uuid | v1.6.0 | BSD |
| github.com/gorilla/websocket | v1.5.3 | BSD |
| github.com/huin/goupnp | v1.2.0 | BSD |
| github.com/jackpal/go-nat-pmp | v1.0.2 | Apache-2.0 |
| github.com/juju/ratelimit | v1.0.2 | LGPL-3.0 (со статическим исключением) |
| github.com/klauspost/compress | v1.17.4 | Apache-2.0 |
| github.com/klauspost/cpuid/v2 | v2.4.0 | MIT |
| github.com/koron/go-ssdp | v0.0.4 | MIT |
| github.com/leaanthony/go-ansi-parser | v1.6.1 | MIT |
| github.com/leaanthony/slicer | v1.6.0 | MIT |
| github.com/leaanthony/u | v1.1.1 | MIT |
| github.com/libp2p/go-nat | v1.0.1-0.20250821073202-01afc089f138 | Apache-2.0 |
| github.com/libp2p/go-netroute | v0.2.1 | BSD |
| github.com/miekg/dns | v1.1.73 | BSD |
| github.com/pelletier/go-toml | v1.9.5 | Apache-2.0 |
| github.com/pion/dtls/v3 | v3.1.5 | MIT |
| github.com/pion/logging | v0.2.4 | MIT |
| github.com/pion/stun/v3 | v3.1.7 | MIT |
| github.com/pion/transport/v4 | v4.1.0 | MIT |
| github.com/pires/go-proxyproto | v0.15.0 | Apache-2.0 |
| github.com/pkg/errors | v0.9.1 | BSD |
| github.com/quic-go/qpack | v0.6.0 | MIT |
| github.com/refraction-networking/utls | v1.8.3-0.20260301010127-aa6edf4b11af | BSD |
| github.com/rivo/uniseg | v0.4.7 | MIT |
| github.com/robfig/cron/v3 | v3.0.1 | MIT |
| github.com/wailsapp/go-webview2 | v1.0.22 | MIT |
| github.com/wailsapp/wails/v2 | v2.16.0 | MIT |
| github.com/wlynxg/anet | v0.0.5 | BSD |
| github.com/xtls/reality | v0.0.0-20260908062103-8cdf7bf9c7f0 | MPL-2.0 |
| github.com/xtls/xray-core | v1.260327.1-0.20261010092107-701af60772cd | MPL-2.0 |
| github.com/yuin/gopher-lua | v1.1.2 | MIT |
| go4.org/netipx | v0.0.0-20231129151722-fdeea329fbba | BSD |
| golang.org/x/crypto | v0.57.0 | BSD |
| golang.org/x/crypto/x509roots/fallback | v0.0.0-20261005185213-c3db4df58582 | BSD |
| golang.org/x/exp | v0.0.0-20240506185415-9bf2ced13842 | BSD |
| golang.org/x/net | v0.59.0 | BSD |
| golang.org/x/sync | v0.23.0 | BSD |
| golang.org/x/sys | v0.49.0 | BSD |
| golang.org/x/text | v0.42.0 | BSD |
| golang.org/x/time | v0.14.0 | BSD |
| golang.zx2c4.com/wintun | v0.0.0-20230126152724-0fa3db229ce2 | MIT |
| golang.zx2c4.com/wireguard | v0.0.0-20250521234502-f333402bd9cb | MIT |
| golang.zx2c4.com/wireguard/windows | v1.1.1 | MIT |
| google.golang.org/genproto/googleapis/rpc | v0.0.0-20260706201446-f0a921348800 | Apache-2.0 |
| google.golang.org/grpc | v1.84.0 | Apache-2.0 |
| google.golang.org/protobuf | v1.36.12 | BSD |
| gopkg.in/yaml.v2 | v2.4.0 | Apache-2.0 |
| gvisor.dev/gvisor | v0.0.0-20260122175437-89a5d21be8f0 | Apache-2.0 |
| layeh.com/gopher-luar | v1.0.11 | MPL-2.0 |
| lukechampine.com/blake3 | v1.4.1 | MIT |
