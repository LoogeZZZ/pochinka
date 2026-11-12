/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2019-2026 WireGuard LLC. All Rights Reserved.
 */

package killswitch

import (
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Известные адреса IPv6 для DHCPv6 и NDP.
var (
	linkLocal = wtFwpV6AddrAndMask{[16]uint8{0xfe, 0x80}, 10}

	linkLocalDHCPMulticast = wtFwpByteArray16{[16]uint8{0xFF, 0x02, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, 0x0, 0x2}}
	siteLocalDHCPMulticast = wtFwpByteArray16{[16]uint8{0xFF, 0x05, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, 0x0, 0x3}}

	linkLocalRouterMulticast = wtFwpByteArray16{[16]uint8{0xFF, 0x02, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x2}}
)

// Правила Kill Switch. Применяются только к исходящим слоям
// ALE_AUTH_CONNECT: входящие соединения не трогаем - утечку создаёт именно
// исходящий трафик мимо VPN, а блокировка входящего ломает доступ к
// компьютеру из локальной сети без пользы для дела.
//
// WFP выбирает фильтр с наибольшим весом среди совпавших, поэтому правила
// разрешений (вес 12–15) всегда побеждают «блокировать всё» (вес 0).

// addFilter добавляет разрешающий фильтр с условиями conditions.
func addFilter(session uintptr, bo *baseObjects, weight uint8, layerName string, name string, conditions []wtFwpmFilterCondition0) error {
	displayData, err := createWtFwpmDisplayData0(name, "")
	if err != nil {
		return wrapErr(err)
	}

	filter := wtFwpmFilter0{
		displayData:         *displayData,
		providerKey:         &bo.provider,
		subLayerKey:         bo.filters,
		layerKey:            layerByName(layerName),
		weight:              filterWeight(weight),
		numFilterConditions: uint32(len(conditions)),
		action: wtFwpmAction0{
			_type: cFWP_ACTION_PERMIT,
		},
	}
	if len(conditions) > 0 {
		filter.filterCondition = &conditions[0]
	}

	filterID := uint64(0)
	return wrapErr(fwpmFilterAdd0(session, &filter, 0, &filterID))
}

// layerByName - GUID исходящего слоя ALE_AUTH_CONNECT по короткому имени.
func layerByName(name string) windows.GUID {
	switch name {
	case "v4":
		return cFWPM_LAYER_ALE_AUTH_CONNECT_V4
	case "v6":
		return cFWPM_LAYER_ALE_AUTH_CONNECT_V6
	}
	return windows.GUID{}
}

// permitSelf - исходящие соединения самой Починки. В её же процессе
// работают ядро xray и прокси Telegram: их трафик ходит в интернет всегда.
func permitSelf(session uintptr, baseObjects *baseObjects, weight uint8) error {
	appID, err := getCurrentProcessAppID()
	if err != nil {
		return wrapErr(err)
	}
	defer fwpmFreeMemory0(unsafe.Pointer(&appID))

	conditions := []wtFwpmFilterCondition0{{
		fieldKey:  cFWPM_CONDITION_ALE_APP_ID,
		matchType: cFWP_MATCH_EQUAL,
		conditionValue: wtFwpConditionValue0{
			_type: cFWP_BYTE_BLOB_TYPE,
			value: uintptr(unsafe.Pointer(appID)),
		},
	}}

	for _, layer := range []string{"v4", "v6"} {
		if err := addFilter(session, baseObjects, weight, layer, "Permit Pochinka outbound ("+layer+")", conditions); err != nil {
			return err
		}
	}
	runtime.KeepAlive(conditions)
	return nil
}

// permitLoopback - локальные соединения (в системном прокси-режиме браузеры
// ходят в xray через 127.0.0.1).
func permitLoopback(session uintptr, baseObjects *baseObjects, weight uint8) error {
	conditions := []wtFwpmFilterCondition0{{
		fieldKey:  cFWPM_CONDITION_FLAGS,
		matchType: cFWP_MATCH_FLAGS_ALL_SET,
		conditionValue: wtFwpConditionValue0{
			_type: cFWP_UINT32,
			value: uintptr(cFWP_CONDITION_FLAG_IS_LOOPBACK),
		},
	}}
	for _, layer := range []string{"v4", "v6"} {
		if err := addFilter(session, baseObjects, weight, layer, "Permit outbound on loopback ("+layer+")", conditions); err != nil {
			return err
		}
	}
	return nil
}

// permitDNS - запросы DNS (порт 53, TCP и UDP) к любым серверам. Без них в
// системном прокси-режиме у системы и фоновых служб не работало бы
// разрешение имён. Повтор условия IP_PROTOCOL - это логическое ИЛИ.
func permitDNS(session uintptr, baseObjects *baseObjects, weight uint8) error {
	conditions := []wtFwpmFilterCondition0{
		{
			fieldKey:  cFWPM_CONDITION_IP_REMOTE_PORT,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_UINT16,
				value: uintptr(53),
			},
		},
		{
			fieldKey:  cFWPM_CONDITION_IP_PROTOCOL,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_UINT8,
				value: uintptr(cIPPROTO_UDP),
			},
		},
		{
			fieldKey:  cFWPM_CONDITION_IP_PROTOCOL,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_UINT8,
				value: uintptr(cIPPROTO_TCP),
			},
		},
	}
	for _, layer := range []string{"v4", "v6"} {
		if err := addFilter(session, baseObjects, weight, layer, "Permit outbound DNS ("+layer+")", conditions); err != nil {
			return err
		}
	}
	return nil
}

// permitTunInterface - трафик программ, который уходит в наш виртуальный
// адаптер TUN (когда VPN в режиме TUN). luid == 0 - не добавляем вовсе.
func permitTunInterface(session uintptr, baseObjects *baseObjects, weight uint8, ifLUID uint64) error {
	if ifLUID == 0 {
		return nil
	}
	conditions := []wtFwpmFilterCondition0{{
		fieldKey:  cFWPM_CONDITION_IP_LOCAL_INTERFACE,
		matchType: cFWP_MATCH_EQUAL,
		conditionValue: wtFwpConditionValue0{
			_type: cFWP_UINT64,
			value: uintptr(unsafe.Pointer(&ifLUID)),
		},
	}}
	for _, layer := range []string{"v4", "v6"} {
		if err := addFilter(session, baseObjects, weight, layer, "Permit outbound on TUN interface ("+layer+")", conditions); err != nil {
			return err
		}
	}
	runtime.KeepAlive(ifLUID)
	return nil
}

// permitDHCPIPv4 - исходящие запросы DHCP, чтобы сеть не отваливалась.
func permitDHCPIPv4(session uintptr, baseObjects *baseObjects, weight uint8) error {
	conditions := []wtFwpmFilterCondition0{
		{
			fieldKey:  cFWPM_CONDITION_IP_PROTOCOL,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_UINT8,
				value: uintptr(cIPPROTO_UDP),
			},
		},
		{
			fieldKey:  cFWPM_CONDITION_IP_LOCAL_PORT,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_UINT16,
				value: uintptr(68),
			},
		},
		{
			fieldKey:  cFWPM_CONDITION_IP_REMOTE_PORT,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_UINT16,
				value: uintptr(67),
			},
		},
		{
			fieldKey:  cFWPM_CONDITION_IP_REMOTE_ADDRESS,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_UINT32,
				value: uintptr(0xffffffff),
			},
		},
	}
	return addFilter(session, baseObjects, weight, "v4", "Permit outbound DHCP request (IPv4)", conditions)
}

// permitDHCPIPv6 - исходящие запросы DHCPv6.
func permitDHCPIPv6(session uintptr, baseObjects *baseObjects, weight uint8) error {
	conditions := []wtFwpmFilterCondition0{
		{
			fieldKey:  cFWPM_CONDITION_IP_PROTOCOL,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_UINT8,
				value: uintptr(cIPPROTO_UDP),
			},
		},
		{
			fieldKey:  cFWPM_CONDITION_IP_REMOTE_ADDRESS,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_BYTE_ARRAY16_TYPE,
				value: uintptr(unsafe.Pointer(&linkLocalDHCPMulticast)),
			},
		},
		// Повтор условия для логического ИЛИ.
		{
			fieldKey:  cFWPM_CONDITION_IP_REMOTE_ADDRESS,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_BYTE_ARRAY16_TYPE,
				value: uintptr(unsafe.Pointer(&siteLocalDHCPMulticast)),
			},
		},
		{
			fieldKey:  cFWPM_CONDITION_IP_REMOTE_PORT,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_UINT16,
				value: uintptr(547),
			},
		},
		{
			fieldKey:  cFWPM_CONDITION_IP_LOCAL_ADDRESS,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_V6_ADDR_MASK,
				value: uintptr(unsafe.Pointer(&linkLocal)),
			},
		},
		{
			fieldKey:  cFWPM_CONDITION_IP_LOCAL_PORT,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_UINT16,
				value: uintptr(546),
			},
		},
	}
	return addFilter(session, baseObjects, weight, "v6", "Permit outbound DHCP request (IPv6)", conditions)
}

// permitNdp - исходящие сообщения обнаружения соседей IPv6 (router
// solicitation, neighbor solicitation/advertisement): без них на сетях с
// IPv6 не было бы ни шлюза, ни соседей.
func permitNdp(session uintptr, baseObjects *baseObjects, weight uint8) error {
	for _, def := range []struct {
		name string
		typ  uint16
	}{
		{"Permit NDP type 133", 133}, // Router Solicitation
		{"Permit NDP type 135", 135}, // Neighbor Solicitation
		{"Permit NDP type 136", 136}, // Neighbor Advertisement
	} {
		conditions := []wtFwpmFilterCondition0{
			{
				fieldKey:  cFWPM_CONDITION_IP_PROTOCOL,
				matchType: cFWP_MATCH_EQUAL,
				conditionValue: wtFwpConditionValue0{
					_type: cFWP_UINT8,
					value: uintptr(cIPPROTO_ICMPV6),
				},
			},
			{
				fieldKey:  cFWPM_CONDITION_ICMP_TYPE,
				matchType: cFWP_MATCH_EQUAL,
				conditionValue: wtFwpConditionValue0{
					_type: cFWP_UINT16,
					value: uintptr(def.typ),
				},
			},
			{
				fieldKey:  cFWPM_CONDITION_ICMP_CODE,
				matchType: cFWP_MATCH_EQUAL,
				conditionValue: wtFwpConditionValue0{
					_type: cFWP_UINT16,
					value: uintptr(0),
				},
			},
		}
		if def.typ == 133 {
			// Router Solicitation идёт только на мультикаст-адрес всех роутеров.
			conditions = append(conditions, wtFwpmFilterCondition0{
				fieldKey:  cFWPM_CONDITION_IP_REMOTE_ADDRESS,
				matchType: cFWP_MATCH_EQUAL,
				conditionValue: wtFwpConditionValue0{
					_type: cFWP_BYTE_ARRAY16_TYPE,
					value: uintptr(unsafe.Pointer(&linkLocalRouterMulticast)),
				},
			})
		}
		if err := addFilter(session, baseObjects, weight, "v6", def.name, conditions); err != nil {
			return err
		}
	}
	return nil
}

// blockAll - блокирует весь остальной исходящий трафик. Разрешения выше по
// весу, поэтому они побеждают.
func blockAll(session uintptr, baseObjects *baseObjects, weight uint8) error {
	filter := wtFwpmFilter0{
		providerKey: &baseObjects.provider,
		subLayerKey: baseObjects.filters,
		weight:      filterWeight(weight),
		action: wtFwpmAction0{
			_type: cFWP_ACTION_BLOCK,
		},
	}

	filterID := uint64(0)

	for _, layer := range []struct {
		key  windows.GUID
		name string
	}{
		{cFWPM_LAYER_ALE_AUTH_CONNECT_V4, "Block all outbound (IPv4)"},
		{cFWPM_LAYER_ALE_AUTH_CONNECT_V6, "Block all outbound (IPv6)"},
	} {
		displayData, err := createWtFwpmDisplayData0(layer.name, "")
		if err != nil {
			return wrapErr(err)
		}
		filter.displayData = *displayData
		filter.layerKey = layer.key
		if err := fwpmFilterAdd0(session, &filter, 0, &filterID); err != nil {
			return wrapErr(err)
		}
	}
	return nil
}
