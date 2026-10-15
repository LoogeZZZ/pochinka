// Package applog — простой логгер: пишет в файл и держит хвост в памяти,
// чтобы показать его в окне приложения.
package applog

import (
	"fmt"
	"os"
	"regexp"
	"sync"
	"time"
)

const (
	ringSize   = 300
	maxLogSize = 2 << 20 // 2 МБ, потом файл начинается заново (старый — .old)
)

var (
	mu       sync.Mutex
	ring     []string
	file     *os.File
	path     string
	written  int64
	listener func(line string)
)

func Init(p string) {
	mu.Lock()
	defer mu.Unlock()
	path = p
	openLocked()
}

func openLocked() {
	if st, err := os.Stat(path); err == nil && st.Size() > maxLogSize {
		_ = os.Remove(path + ".old")
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		file = nil
		return
	}
	file = f
	written = 0
	if st, err := f.Stat(); err == nil {
		written = st.Size()
	}
}

// OnLine регистрирует колбэк на каждую новую строку (для UI).
func OnLine(fn func(line string)) {
	mu.Lock()
	listener = fn
	mu.Unlock()
}

// Секреты, которые не должны попасть в журнал, даже если просочатся в
// текст ошибки: ссылки подписок с токеном, секрет прокси Telegram, UUID.
var redactors = []struct {
	re   *regexp.Regexp
	with string
}{
	{regexp.MustCompile(`(?i)(https?://[^/\s"']+)/[^\s"']+`), "$1/***"},
	{regexp.MustCompile(`(?i)(secret=)(dd|ee)?[0-9a-f]{16,}`), "$1***"},
	{regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`), "********-****"},
	{regexp.MustCompile(`(?i)(vless|vmess|trojan|ss|hysteria2|hy2)://[^\s"']+`), "$1://***"},
}

func redact(s string) string {
	for _, r := range redactors {
		s = r.re.ReplaceAllString(s, r.with)
	}
	return s
}

func Printf(format string, args ...any) {
	line := time.Now().Format("15:04:05") + "  " + redact(fmt.Sprintf(format, args...))
	mu.Lock()
	ring = append(ring, line)
	if len(ring) > ringSize {
		ring = ring[len(ring)-ringSize:]
	}
	if file != nil {
		n, _ := fmt.Fprintln(file, time.Now().Format("2006-01-02 ")+line)
		written += int64(n)
		// Ротация на ходу: winws пишет постоянно, ждать перезапуска нельзя.
		if written > maxLogSize {
			file.Close()
			openLocked()
		}
	}
	l := listener
	mu.Unlock()
	if l != nil {
		l(line)
	}
}

func Tail() []string {
	mu.Lock()
	defer mu.Unlock()
	return append([]string(nil), ring...)
}

func Close() {
	mu.Lock()
	defer mu.Unlock()
	if file != nil {
		file.Close()
		file = nil
	}
}
