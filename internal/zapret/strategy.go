package zapret

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Strategy — одна стратегия обхода, взятая из .bat-файла релиза.
type Strategy struct {
	Name string // "general (ALT2)"
	File string // полный путь к .bat
}

// ListStrategies возвращает стратегии из распакованного релиза в порядке,
// удобном человеку: general, затем ALT, ALT2, ... ALT13, затем остальные.
func ListStrategies(dir string) ([]Strategy, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.bat"))
	if err != nil {
		return nil, err
	}
	var out []Strategy
	for _, m := range matches {
		name := strings.TrimSuffix(filepath.Base(m), filepath.Ext(m))
		if strings.HasPrefix(strings.ToLower(name), "service") {
			continue
		}
		if !batLaunchesWinws(m) {
			continue
		}
		out = append(out, Strategy{Name: name, File: m})
	}
	sort.Slice(out, func(i, j int) bool { return strategyLess(out[i].Name, out[j].Name) })
	return out, nil
}

func batLaunchesWinws(path string) bool {
	b, err := os.ReadFile(path)
	return err == nil && bytes.Contains(bytes.ToLower(b), []byte(`winws.exe"`))
}

// strategyLess: "general" первым, ALT-N по числу, остальные по алфавиту.
func strategyLess(a, b string) bool {
	ka, na := strategyKey(a)
	kb, nb := strategyKey(b)
	if ka != kb {
		return ka < kb
	}
	if na != nb {
		return na < nb
	}
	return a < b
}

func strategyKey(name string) (group int, num int) {
	l := strings.ToLower(name)
	if l == "general" {
		return 0, 0
	}
	if strings.HasPrefix(l, "general (alt") && strings.HasSuffix(l, ")") {
		digits := strings.TrimSuffix(strings.TrimPrefix(l, "general (alt"), ")")
		n := 1
		if digits != "" {
			n = 0
			for _, r := range digits {
				if r < '0' || r > '9' {
					return 2, 0
				}
				n = n*10 + int(r-'0')
			}
		}
		return 1, n
	}
	return 2, 0
}

// Vars — значения переменных, которые .bat получает от service.bat.
type Vars struct {
	BatDir        string // %~dp0 — папка релиза
	GameFilterTCP string
	GameFilterUDP string
}

// GameFilterPorts повторяет логику :game_switch_status из service.bat.
// "12" — заглушка: порт, на котором заведомо ничего нет, чтобы фильтр был
// синтаксически валиден, но фактически выключен.
func GameFilterPorts(mode string) (tcp, udp string) {
	const off, full = "12", "1024-65535"
	switch strings.ToLower(mode) {
	case "all":
		return full, full
	case "tcp":
		return full, off
	case "udp":
		return off, full
	}
	return off, off
}

// asciiLower — нижний регистр только для ASCII: длина строки не меняется
// (strings.ToLower заменяет битые байты UTF-8 на трёхбайтовый U+FFFD, и
// найденный индекс не совпал бы с исходной строкой).
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// ParseBat извлекает аргументы winws.exe из .bat-стратегии.
//
// Формат строки в релизе:
//
//	start "zapret: %~n0" /min "%BIN%winws.exe" --wf-tcp=... ^
//	--filter-udp=443 --hostlist="%LISTS%list-general.txt" ... ^
//	...последняя строка без ^
//
// Мы склеиваем продолжения (^ в конце строки), подставляем переменные так же,
// как это сделал бы cmd.exe, и режем по пробелам с учётом кавычек.
func ParseBat(path string, v Vars) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})

	var cmdline strings.Builder
	capturing := false
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), " \t\r")
		if !capturing {
			idx := strings.Index(asciiLower(line), `winws.exe"`)
			if idx < 0 {
				continue
			}
			capturing = true
			line = line[idx+len(`winws.exe"`):]
		}
		cont := strings.HasSuffix(line, "^")
		if cont {
			line = strings.TrimSuffix(line, "^")
		}
		cmdline.WriteString(line)
		cmdline.WriteByte(' ')
		if !cont {
			break
		}
	}
	if !capturing {
		return nil, errors.New("в файле не найден запуск winws.exe")
	}

	batDir := v.BatDir
	if !strings.HasSuffix(batDir, `\`) {
		batDir += `\`
	}
	repl := strings.NewReplacer(
		"%BIN%", batDir+`bin\`,
		"%LISTS%", batDir+`lists\`,
		"%~dp0", batDir,
		"%GameFilterTCP%", v.GameFilterTCP,
		"%GameFilterUDP%", v.GameFilterUDP,
		"%GameFilter%", v.GameFilterTCP,
	)
	expanded := repl.Replace(cmdline.String())
	if i := strings.Index(expanded, "%"); i >= 0 {
		// Неизвестная переменная — лучше упасть явно, чем запустить winws
		// с мусором в аргументах.
		end := strings.Index(expanded[i+1:], "%")
		if end >= 0 {
			return nil, errors.New("неизвестная переменная в стратегии: " + expanded[i:i+end+2])
		}
	}
	args := splitArgs(expanded)
	if len(args) == 0 {
		return nil, errors.New("стратегия без аргументов")
	}
	return args, nil
}

// splitArgs режет строку по пробелам вне кавычек и убирает сами кавычки.
// --hostlist="C:\x\list.txt" превращается в --hostlist=C:\x\list.txt;
// при запуске Go сам заэкранирует аргумент, если в нём будут пробелы.
func splitArgs(s string) []string {
	var args []string
	var cur strings.Builder
	inQuote, has, escaped := false, false, false
	for _, r := range s {
		switch {
		case escaped:
			// ^x вне кавычек в cmd.exe — это буквальный x (например, ^! → !).
			cur.WriteRune(r)
			has, escaped = true, false
		case r == '^' && !inQuote:
			escaped = true
		case r == '"':
			inQuote = !inQuote
			has = true
		case (r == ' ' || r == '\t') && !inQuote:
			if has {
				args = append(args, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	if has {
		args = append(args, cur.String())
	}
	return args
}
