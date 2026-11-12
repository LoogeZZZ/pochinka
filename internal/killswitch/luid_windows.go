//go:build windows

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2019-2026 WireGuard LLC. All Rights Reserved.
 */

package killswitch

import (
	"net"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iphlpapiKillswitch = windows.NewLazySystemDLL("iphlpapi.dll")
	convIndexToLuid    = iphlpapiKillswitch.NewProc("ConvertInterfaceIndexToLuid")
)

// LuidFromIndex - LUID сетевого адаптера по его индексу (нужен условию
// WFP IP_LOCAL_INTERFACE для виртуального адаптера TUN).
func LuidFromIndex(idx int) (uint64, error) {
	var luid uint64
	r, _, _ := convIndexToLuid.Call(uintptr(uint32(idx)), uintptr(unsafe.Pointer(&luid)))
	if r != 0 {
		return 0, syscall.Errno(r)
	}
	return luid, nil
}

// LuidFromName - LUID адаптера по его имени.
func LuidFromName(name string) (uint64, error) {
	ifc, err := net.InterfaceByName(name)
	if err != nil {
		return 0, err
	}
	return LuidFromIndex(ifc.Index)
}
