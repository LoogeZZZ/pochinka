// Package tgproxy — локальный MTProto-прокси для Telegram Desktop, перенос
// ядра github.com/Flowseal/tg-ws-proxy (MIT) на Go.
//
// Telegram подключается к 127.0.0.1 как к обычному MTProto-прокси (секрет
// "dd…"). Прокси расшифровывает obfuscated2-заголовок, узнаёт номер
// дата-центра и пересылает трафик в Telegram через WebSocket
// (wss://kwsN.web.telegram.org/apiws) — тот же канал, что использует
// web.telegram.org, — перешифровывая поток между двумя сторонами.
package tgproxy

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
)

const (
	handshakeLen = 64
	skipLen      = 8
	prekeyLen    = 32
	keyLen       = 32
	ivLen        = 16
	protoTagPos  = 56
	dcIdxPos     = 60
)

var (
	tagAbridged     = [4]byte{0xef, 0xef, 0xef, 0xef}
	tagIntermediate = [4]byte{0xee, 0xee, 0xee, 0xee}
	tagSecure       = [4]byte{0xdd, 0xdd, 0xdd, 0xdd}
)

// Первые 4 байта, которыми не может начинаться obfuscated2-заголовок
// (иначе его спутают с HTTP, TLS или нешифрованным транспортом).
var reservedStarts = [][4]byte{
	{'H', 'E', 'A', 'D'}, {'P', 'O', 'S', 'T'}, {'G', 'E', 'T', ' '}, {'O', 'P', 'T', 'I'},
	{0xee, 0xee, 0xee, 0xee}, {0xdd, 0xdd, 0xdd, 0xdd}, {0x16, 0x03, 0x01, 0x02},
}

// clientHello — разобранный заголовок клиента.
type clientHello struct {
	dc       int // номер DC (1..5, 203); для тестовых — +10000
	media    bool
	protoTag [4]byte
	prekeyIV []byte // 48 байт (prekey+iv) из заголовка — для ключей ответа
}

func newCTR(key, iv []byte) cipher.Stream {
	b, err := aes.NewCipher(key)
	if err != nil {
		panic(err) // длина ключа фиксированная (32), сюда не попадаем
	}
	return cipher.NewCTR(b, iv)
}

// parseClientHello расшифровывает 64-байтный заголовок ключом из секрета.
// nil — заголовок не наш (неверный секрет или протокол).
func parseClientHello(hs []byte, secret []byte) *clientHello {
	if len(hs) != handshakeLen {
		return nil
	}
	prekeyIV := hs[skipLen : skipLen+prekeyLen+ivLen]
	h := sha256.Sum256(append(append([]byte{}, prekeyIV[:prekeyLen]...), secret...))
	dec := make([]byte, handshakeLen)
	newCTR(h[:], prekeyIV[prekeyLen:]).XORKeyStream(dec, hs)

	var tag [4]byte
	copy(tag[:], dec[protoTagPos:protoTagPos+4])
	if tag != tagAbridged && tag != tagIntermediate && tag != tagSecure {
		return nil
	}
	idx := int(int16(binary.LittleEndian.Uint16(dec[dcIdxPos : dcIdxPos+2])))
	dc, media := idx, false
	if idx < 0 {
		dc, media = -idx, true
	}
	if dc == 0 {
		return nil
	}
	return &clientHello{dc: dc, media: media, protoTag: tag, prekeyIV: append([]byte{}, prekeyIV...)}
}

// relayInit — наш собственный obfuscated2-заголовок для Telegram (без
// секрета: на стороне Telegram это обычный obfuscated2).
func relayInit(tag [4]byte, dcIdx int) ([]byte, error) {
	rnd := make([]byte, handshakeLen)
	for {
		if _, err := rand.Read(rnd); err != nil {
			return nil, err
		}
		if rnd[0] == 0xef {
			continue
		}
		var first [4]byte
		copy(first[:], rnd[:4])
		bad := false
		for _, r := range reservedStarts {
			if first == r {
				bad = true
			}
		}
		if bad || (rnd[4] == 0 && rnd[5] == 0 && rnd[6] == 0 && rnd[7] == 0) {
			continue
		}
		break
	}
	key := rnd[skipLen : skipLen+prekeyLen]
	iv := rnd[skipLen+prekeyLen : skipLen+prekeyLen+ivLen]
	enc := make([]byte, handshakeLen)
	newCTR(key, iv).XORKeyStream(enc, rnd)

	tail := make([]byte, 8)
	copy(tail, tag[:])
	binary.LittleEndian.PutUint16(tail[4:], uint16(int16(dcIdx)))
	if _, err := rand.Read(tail[6:]); err != nil {
		return nil, err
	}
	out := append([]byte{}, rnd...)
	for i := 0; i < 8; i++ {
		// ключевой поток = enc ^ rnd; шифруем хвост тем же потоком
		out[protoTagPos+i] = tail[i] ^ (enc[protoTagPos+i] ^ rnd[protoTagPos+i])
	}
	return out, nil
}

// cryptoCtx — четыре потока шифрования моста.
type cryptoCtx struct {
	cltDec cipher.Stream // расшифровать от клиента
	cltEnc cipher.Stream // зашифровать клиенту
	tgEnc  cipher.Stream // зашифровать в Telegram
	tgDec  cipher.Stream // расшифровать от Telegram
}

func reverse(b []byte) []byte {
	out := make([]byte, len(b))
	for i := range b {
		out[i] = b[len(b)-1-i]
	}
	return out
}

func newCryptoCtx(h *clientHello, secret, relay []byte) *cryptoCtx {
	sum := func(prekey []byte) []byte {
		s := sha256.Sum256(append(append([]byte{}, prekey...), secret...))
		return s[:]
	}
	cltDec := newCTR(sum(h.prekeyIV[:prekeyLen]), h.prekeyIV[prekeyLen:])
	rev := reverse(h.prekeyIV)
	cltEnc := newCTR(sum(rev[:prekeyLen]), rev[prekeyLen:])
	skip := make([]byte, handshakeLen)
	cltDec.XORKeyStream(skip, skip) // пропустить 64 байта заголовка

	rk := relay[skipLen : skipLen+prekeyLen+ivLen]
	tgEnc := newCTR(rk[:keyLen], rk[keyLen:])
	rrev := reverse(rk)
	tgDec := newCTR(rrev[:keyLen], rrev[keyLen:])
	skip2 := make([]byte, handshakeLen)
	tgEnc.XORKeyStream(skip2, skip2)
	return &cryptoCtx{cltDec: cltDec, cltEnc: cltEnc, tgEnc: tgEnc, tgDec: tgDec}
}

var errBadSecret = errors.New("секрет должен быть 16 байт")
