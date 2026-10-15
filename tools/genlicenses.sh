#!/usr/bin/env bash
# Собирает third_party/THIRD_PARTY_LICENSES.txt — полные тексты лицензий
# всего, что попадает в exe. Запускать из корня проекта после изменения
# зависимостей (go.mod) или вшитых файлов:
#
#   bash tools/genlicenses.sh
#
# Тексты Go-модулей берутся из кеша модулей (go mod download), остальные —
# из third_party/*.txt|md (скачаны из репозиториев авторов).
set -euo pipefail
cd "$(dirname "$0")/.."

out=third_party/THIRD_PARTY_LICENSES.txt
sep() { printf '\n\n================================================================================\n%s\n================================================================================\n\n' "$1"; }

{
  echo "Починка — лицензии сторонних компонентов"
  echo "Сводка: THIRD_PARTY_NOTICES.md. Ниже — полные тексты."

  sep "Flowseal/tg-ws-proxy — портирован на Go (internal/tgproxy) — MIT"
  cat third_party/tg-ws-proxy-LICENSE.txt
  sep "Flowseal/zapret-discord-youtube, bol-van/zapret — формат стратегий, списки; скачивается при запуске — MIT"
  cat third_party/zapret-discord-youtube-LICENSE.txt
  sep "Wintun (wintun.dll, WireGuard LLC) — вшит без изменений"
  cat third_party/wintun-prebuilt-LICENSE.txt
  sep "runetfreedom/russia-blocked-geoip, russia-blocked-geosite (geoip-ru-only.dat, geosite-ru-only.dat) — данные, вшиты без изменений — GPL-3.0
Исходники: https://github.com/runetfreedom/russia-blocked-geoip
           https://github.com/runetfreedom/russia-blocked-geosite"
  cat third_party/runetfreedom-geo-GPL-3.0.txt
  sep "Twemoji Country Flags (talkjs/country-flag-emoji-polyfill) — шрифт флагов — MIT (код) / CC-BY-4.0 (графика Twemoji)"
  cat third_party/twemoji-country-flags-LICENSE.md

  go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}}{{end}}{{end}}' . | sort -u |
  while read -r mod ver; do
    dir=$(go list -m -f '{{.Dir}}' "$mod")
    lic=$(ls "$dir" | grep -iE '^(licen[cs]e|copying)' | head -1 || true)
    sep "$mod $ver"
    if [ -n "$lic" ]; then
      cat "$dir/$lic"
      # NOTICE у Apache-2.0 тоже обязателен к распространению.
      notice=$(ls "$dir" | grep -iE '^notice' | head -1 || true)
      if [ -n "$notice" ]; then echo; echo "--- NOTICE ---"; cat "$dir/$notice"; fi
    else
      echo "(файл лицензии в модуле не найден — см. https://$mod)"
    fi
  done
} > "$out"

echo "готово: $out ($(wc -c < "$out") байт)"
