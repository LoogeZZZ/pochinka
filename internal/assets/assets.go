// Package assets — иконки, вшитые в exe. Перегенерировать: go run ./tools/genicon
package assets

import _ "embed"

var (
	//go:embed app.ico
	AppIcon []byte
	//go:embed tray_on.ico
	TrayOn []byte
	//go:embed tray_off.ico
	TrayOff []byte
	//go:embed tray_err.ico
	TrayErr []byte

	// Wintun 0.14.1 (amd64) с wintun.net, подпись WireGuard LLC. Нужен для
	// TUN-режима VPN, кладётся рядом с exe при включении TUN.
	//go:embed wintun.dll
	Wintun []byte

	// Стартовая копия баз блокировок runetfreedom (ru-only, gzip): первое
	// подключение VPN не ждёт GitHub, свежие базы подтягиваются в фоне.
	//go:embed geosite.dat.gz
	GeoSiteGz []byte
	//go:embed geoip.dat.gz
	GeoIPGz []byte
)
