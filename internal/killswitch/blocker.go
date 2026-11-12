/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2019-2026 WireGuard LLC. All Rights Reserved.
 */

// Package killswitch - блокировка всего исходящего трафика мимо VPN через
// Windows Filtering Platform (WFP). Пока VPN включён, разрешены только:
// сам процесс Починки (в нём xray и прокси Telegram), loopback, DNS, DHCP,
// NDP и, в режиме TUN, трафик в виртуальный адаптер.
//
// Сессия WFP динамическая: при завершении процесса - даже аварийном -
// фильтры исчезают сами, интернет у пользователя не остаётся заблокирован.
//
// Код основан на пакете tunnel/firewall из WireGuard for Windows (MIT,
// WireGuard LLC), адаптирован: разрешение процесса по пути exe вместо
// SID службы и только исходящие слои ALE_AUTH_CONNECT.
package killswitch

import (
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

type wfpObjectInstaller func(uintptr) error

// Fundamental WFP objects.
type baseObjects struct {
	provider windows.GUID
	filters  windows.GUID
}

var (
	ksMu       sync.Mutex
	wfpSession uintptr
)

func createWfpSession() (uintptr, error) {
	sessionDisplayData, err := createWtFwpmDisplayData0("Pochinka", "Pochinka dynamic session")
	if err != nil {
		return 0, wrapErr(err)
	}

	session := wtFwpmSession0{
		displayData:          *sessionDisplayData,
		flags:                cFWPM_SESSION_FLAG_DYNAMIC,
		txnWaitTimeoutInMSec: windows.INFINITE,
	}

	sessionHandle := uintptr(0)

	err = fwpmEngineOpen0(nil, cRPC_C_AUTHN_WINNT, nil, &session, unsafe.Pointer(&sessionHandle))
	if err != nil {
		return 0, wrapErr(err)
	}

	return sessionHandle, nil
}

func registerBaseObjects(session uintptr) (*baseObjects, error) {
	bo := &baseObjects{}
	var err error
	bo.provider, err = windows.GenerateGUID()
	if err != nil {
		return nil, wrapErr(err)
	}
	bo.filters, err = windows.GenerateGUID()
	if err != nil {
		return nil, wrapErr(err)
	}

	// Register provider.
	{
		displayData, err := createWtFwpmDisplayData0("Pochinka", "Pochinka kill switch provider")
		if err != nil {
			return nil, wrapErr(err)
		}
		provider := wtFwpmProvider0{
			providerKey: bo.provider,
			displayData: *displayData,
		}
		err = fwpmProviderAdd0(session, &provider, 0)
		if err != nil {
			return nil, wrapErr(err)
		}
	}

	// Register filters sublayer.
	{
		displayData, err := createWtFwpmDisplayData0("Pochinka kill switch", "Permissive and blocking filters")
		if err != nil {
			return nil, wrapErr(err)
		}
		sublayer := wtFwpmSublayer0{
			subLayerKey: bo.filters,
			displayData: *displayData,
			providerKey: &bo.provider,
			weight:      ^uint16(0),
		}
		err = fwpmSubLayerAdd0(session, &sublayer, 0)
		if err != nil {
			return nil, wrapErr(err)
		}
	}

	return bo, nil
}

// Enable включает Kill Switch: весь исходящий трафик блокируется, кроме
// трафика Починки (в ней - xray и прокси Telegram), loopback, DNS, DHCP,
// NDP и, если VPN в режиме TUN, трафика в виртуальный адаптер.
//
// Сессия динамическая: при завершении процесса - даже аварийном - фильтры
// исчезают сами, и интернет у пользователя не остаётся «заблокированным
// навсегда». Повторный вызов при включённом Kill Switch - не ошибка.
func Enable(tunLUID uint64) error {
	ksMu.Lock()
	defer ksMu.Unlock()
	if wfpSession != 0 {
		return nil
	}

	session, err := createWfpSession()
	if err != nil {
		return wrapErr(err)
	}

	objectInstaller := func(session uintptr) error {
		baseObjects, err := registerBaseObjects(session)
		if err != nil {
			return wrapErr(err)
		}

		err = permitSelf(session, baseObjects, 15)
		if err != nil {
			return wrapErr(err)
		}

		err = permitLoopback(session, baseObjects, 13)
		if err != nil {
			return wrapErr(err)
		}

		err = permitDNS(session, baseObjects, 12)
		if err != nil {
			return wrapErr(err)
		}

		err = permitDHCPIPv4(session, baseObjects, 12)
		if err != nil {
			return wrapErr(err)
		}

		err = permitDHCPIPv6(session, baseObjects, 12)
		if err != nil {
			return wrapErr(err)
		}

		err = permitNdp(session, baseObjects, 12)
		if err != nil {
			return wrapErr(err)
		}

		err = permitTunInterface(session, baseObjects, 12, tunLUID)
		if err != nil {
			return wrapErr(err)
		}

		err = blockAll(session, baseObjects, 0)
		if err != nil {
			return wrapErr(err)
		}

		return nil
	}

	err = runTransaction(session, objectInstaller)
	if err != nil {
		fwpmEngineClose0(session)
		return wrapErr(err)
	}

	wfpSession = session
	return nil
}

// Disable выключает Kill Switch. Безопасно вызывать и когда он выключен.
func Disable() {
	ksMu.Lock()
	defer ksMu.Unlock()
	if wfpSession != 0 {
		fwpmEngineClose0(wfpSession)
		wfpSession = 0
	}
}

// Active - включён ли Kill Switch прямо сейчас.
func Active() bool {
	ksMu.Lock()
	defer ksMu.Unlock()
	return wfpSession != 0
}
