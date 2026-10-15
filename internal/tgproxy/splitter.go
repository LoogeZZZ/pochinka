package tgproxy

import "encoding/binary"

// splitter режет поток на отдельные транспортные пакеты MTProto, чтобы
// каждый уходил в Telegram отдельным WebSocket-кадром (так делает
// web.telegram.org; сервер ждёт кадр = пакет).
//
// Режем по расшифрованному (plain) потоку, а отдаём зашифрованные для
// Telegram байты тех же позиций. Если формат не распознан — отключаемся и
// дальше шлём как есть.
type splitter struct {
	tag      [4]byte
	cipher   []byte
	plain    []byte
	disabled bool
}

// Защита от мусора и от раздувания памяти злым локальным клиентом.
const maxPacket = 4 << 20 // Telegram шлёт файлы частями по 512 КБ

func (s *splitter) split(chunk, plain []byte) [][]byte {
	if len(chunk) == 0 {
		return nil
	}
	if s.disabled {
		return [][]byte{chunk}
	}
	s.cipher = append(s.cipher, chunk...)
	s.plain = append(s.plain, plain...)
	var parts [][]byte
	off := 0
	for off < len(s.cipher) {
		n, ok := s.nextLen(off)
		if !ok {
			break
		}
		if n <= 0 {
			parts = append(parts, append([]byte{}, s.cipher[off:]...))
			off = len(s.cipher)
			s.disabled = true
			break
		}
		parts = append(parts, append([]byte{}, s.cipher[off:off+n]...))
		off += n
	}
	if off > 0 {
		s.cipher = append(s.cipher[:0], s.cipher[off:]...)
		s.plain = append(s.plain[:0], s.plain[off:]...)
	}
	return parts
}

// flush — остаток при закрытии соединения.
func (s *splitter) flush() []byte {
	if len(s.cipher) == 0 {
		return nil
	}
	t := append([]byte{}, s.cipher...)
	s.cipher, s.plain = s.cipher[:0], s.plain[:0]
	return t
}

// nextLen: длина следующего пакета; ok=false — ещё не хватает данных;
// n<=0 — формат не распознан.
func (s *splitter) nextLen(off int) (n int, ok bool) {
	avail := len(s.plain) - off
	if avail <= 0 {
		return 0, false
	}
	switch s.tag {
	case tagAbridged:
		first := s.plain[off]
		var payload, header int
		if first == 0x7f || first == 0xff {
			if avail < 4 {
				return 0, false
			}
			payload = int(uint32(s.plain[off+1])|uint32(s.plain[off+2])<<8|uint32(s.plain[off+3])<<16) * 4
			header = 4
		} else {
			payload = int(first&0x7f) * 4
			header = 1
		}
		if payload <= 0 || payload > maxPacket {
			return 0, true
		}
		if avail < header+payload {
			return 0, false
		}
		return header + payload, true
	case tagIntermediate, tagSecure:
		if avail < 4 {
			return 0, false
		}
		payload := int(binary.LittleEndian.Uint32(s.plain[off:]) & 0x7fffffff)
		if payload <= 0 || payload > maxPacket {
			return 0, true
		}
		if avail < 4+payload {
			return 0, false
		}
		return 4 + payload, true
	}
	return 0, true
}
