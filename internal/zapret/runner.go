package zapret

import (
	"bufio"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"pochinka/internal/applog"
	"pochinka/internal/winutil"
)

// runner держит один процесс winws и перезапускает его, если тот упал.
type runner struct {
	job *winutil.Job

	mu      sync.Mutex
	cmd     *exec.Cmd
	stopped chan struct{} // закрывается, когда мы сами просим остановиться
	done    chan struct{} // закрывается, когда супервизор вышел
	onState func(running bool, err error)
}

func newRunner(onState func(bool, error)) (*runner, error) {
	job, err := winutil.NewKillOnCloseJob()
	if err != nil {
		return nil, err
	}
	return &runner{job: job, onState: onState}, nil
}

// start запускает winws из dir с аргументами args. Возвращает ошибку, если
// процесс не прожил и пары секунд (типичный случай: WinDivert не загрузился
// или антивирус снёс файл) — тогда смысла крутить перезапуски нет.
func (r *runner) start(dir string, args []string) error {
	r.stop()

	exe := filepath.Join(dir, "bin", "winws.exe")
	firstErr := make(chan error, 1)
	r.mu.Lock()
	r.stopped = make(chan struct{})
	r.done = make(chan struct{})
	stopped, done := r.stopped, r.done
	r.mu.Unlock()

	go r.supervise(exe, dir, args, stopped, done, firstErr)
	return <-firstErr
}

func (r *runner) supervise(exe, dir string, args []string, stopped, done chan struct{}, firstErr chan<- error) {
	defer close(done)
	backoff := time.Second
	reported := false
	report := func(err error) {
		if !reported {
			reported = true
			firstErr <- err
		}
	}
	fastFails := 0

	for {
		cmd := exec.Command(exe, args...)
		cmd.Dir = filepath.Join(dir, "bin")
		winutil.HideWindow(cmd)
		out, _ := cmd.StdoutPipe()
		cmd.Stderr = cmd.Stdout

		startedAt := time.Now()
		if err := cmd.Start(); err != nil {
			report(err)
			r.notify(false, err)
			return
		}
		if err := r.job.Assign(cmd.Process.Pid); err != nil {
			applog.Printf("zapret: не удалось привязать winws к job: %v", err)
		}
		r.mu.Lock()
		r.cmd = cmd
		r.mu.Unlock()

		tail := &lineTail{}
		go pump(out, tail)

		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()

		// Считаем запуск удачным, если winws прожил 2 секунды.
		select {
		case <-time.After(2 * time.Second):
			applog.Printf("zapret: winws запущен (pid %d)", cmd.Process.Pid)
			report(nil)
			r.notify(true, nil)
		case err := <-exited:
			msg := tail.String()
			e := explainExit(err, msg)
			applog.Printf("zapret: winws сразу завершился: %v", e)
			select {
			case <-stopped:
				report(nil)
				return
			default:
			}
			if !reported {
				report(e)
				r.notify(false, e)
				return
			}
			exited <- err // передаём ниже как обычное падение
		}

		select {
		case <-stopped:
			_ = cmd.Process.Kill()
			<-exited
			r.notify(false, nil)
			return
		case err := <-exited:
			select {
			case <-stopped:
				r.notify(false, nil)
				return
			default:
			}
			e := explainExit(err, tail.String())
			applog.Printf("zapret: winws упал: %v — перезапуск через %v", e, backoff)
			r.notify(false, e)
			if time.Since(startedAt) < 10*time.Second {
				fastFails++
			} else {
				fastFails, backoff = 0, time.Second
			}
			if fastFails >= 5 {
				applog.Printf("zapret: winws падает раз за разом, прекращаю перезапуски")
				return
			}
		}

		select {
		case <-stopped:
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (r *runner) notify(running bool, err error) {
	if r.onState != nil {
		r.onState(running, err)
	}
}

// stop останавливает winws и ждёт завершения супервизора.
func (r *runner) stop() {
	r.mu.Lock()
	stopped, done, cmd := r.stopped, r.done, r.cmd
	r.stopped, r.done, r.cmd = nil, nil, nil
	r.mu.Unlock()
	if stopped == nil {
		return
	}
	close(stopped)
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	<-done
}

func (r *runner) running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopped != nil
}

// pump читает вывод winws в лог и запоминает последние строки для диагностики.
func pump(rd io.Reader, tail *lineTail) {
	sc := bufio.NewScanner(rd)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		tail.add(line)
		applog.Printf("winws: %s", line)
	}
}

type lineTail struct {
	mu    sync.Mutex
	lines []string
}

func (t *lineTail) add(s string) {
	t.mu.Lock()
	t.lines = append(t.lines, s)
	if len(t.lines) > 8 {
		t.lines = t.lines[1:]
	}
	t.mu.Unlock()
}

func (t *lineTail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.Join(t.lines, " | ")
}

// explainExit переводит типичные причины падения winws на человеческий.
func explainExit(err error, output string) error {
	low := strings.ToLower(output)
	switch {
	case strings.Contains(low, "windivert") && (strings.Contains(low, "access is denied") || strings.Contains(low, "error 5")):
		return errors.New("WinDivert: нет прав администратора")
	case strings.Contains(low, "windivert") && strings.Contains(low, "577"):
		return errors.New("WinDivert: драйвер заблокирован (проверь Secure Boot / антивирус)")
	case strings.Contains(low, "windivert") && strings.Contains(low, "1275"):
		return errors.New("WinDivert: драйвер заблокирован системой — нужна перезагрузка или удаление конфликтующей программы")
	case strings.Contains(low, "windivert"):
		return errors.New("WinDivert не запустился: " + output)
	}
	if output != "" {
		return errors.New(output)
	}
	if err != nil {
		return err
	}
	return errors.New("winws завершился без объяснений")
}
