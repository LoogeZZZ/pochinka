package vpn

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pochinka/internal/config"
	"pochinka/internal/paths"
)

// subscription — одна подписка со всем её состоянием. Каждая лежит в своём
// файле, поэтому при переключении между подписками и после перезапуска
// приложения всё возвращается как было: выбранный сервер, пинги, трафик.
type subscription struct {
	ID       string         `json:"id"`
	URL      string         `json:"url"`
	Added    int64          `json:"added"`
	Info     SubInfo        `json:"info"`
	Selected string         `json:"selected"` // ID сервера или AutoServer
	Pings    map[string]int `json:"pings"`
	PingedAt int64          `json:"pingedAt"`
	Servers  []cachedServer `json:"servers"`

	servers []*Server // разобранные Servers
	removed bool      // подписку удалили — фоновые операции не должны её воскресить
}

// SubView — подписка для окна (без ссылки: в ней токен доступа).
type SubView struct {
	ID      string  `json:"id"`
	Title   string  `json:"title"`
	Host    string  `json:"host"`
	Info    SubInfo `json:"info"`
	Servers int     `json:"servers"`
	Active  bool    `json:"active"`
}

func subsDir() string { return filepath.Join(paths.DataDir(), "vpn-subs") }

func newSubID() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *subscription) title() string {
	if t := strings.TrimSpace(s.Info.Title); t != "" {
		return t
	}
	return subHost(s.URL)
}

func (s *subscription) setServers(list []*Server) {
	s.servers = list
	s.Servers = cacheOf(list)
}

func (s *subscription) find(id string) *Server {
	for _, sv := range s.servers {
		if sv.ID == id {
			return sv
		}
	}
	return nil
}

func (s *subscription) save() error {
	if s.removed {
		return nil
	}
	if err := os.MkdirAll(subsDir(), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(subsDir(), s.ID+".json"), b)
}

func (s *subscription) remove() { _ = os.Remove(filepath.Join(subsDir(), s.ID+".json")) }

// loadSubs читает все подписки с диска (по порядку добавления) и переносит
// старую одиночную подписку из прошлых версий.
func loadSubs() []*subscription {
	migrateLegacySub()
	files, _ := filepath.Glob(filepath.Join(subsDir(), "*.json"))
	var out []*subscription
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var s subscription
		if json.Unmarshal(b, &s) != nil || s.ID == "" || s.URL == "" {
			continue
		}
		var list []*Server
		for _, cs := range s.Servers {
			sv := cs.Server
			sv.Outbound, sv.Extra = cs.Outbound, cs.Extra
			list = append(list, &sv)
		}
		s.servers, _ = validateServers(list) // кэш мог остаться от старой версии ядра
		if s.Pings == nil {
			s.Pings = map[string]int{}
		}
		if s.Selected == "" || (s.Selected != AutoServer && s.find(s.Selected) == nil) {
			s.Selected = AutoServer
		}
		out = append(out, &s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Added < out[j].Added })
	return out
}

// migrateLegacySub: до мульти-подписок была одна подписка в config.json +
// vpn-cache.json. Переносим её в новый формат один раз.
func migrateLegacySub() {
	cfg := config.Load()
	if cfg.VPNSubURL == "" {
		return
	}
	s := &subscription{ID: newSubID(), URL: cfg.VPNSubURL, Added: time.Now().Unix(), Selected: AutoServer, Pings: map[string]int{}}
	legacy := filepath.Join(paths.DataDir(), "vpn-cache.json")
	if b, err := os.ReadFile(legacy); err == nil {
		var c cache
		if json.Unmarshal(b, &c) == nil {
			s.Info, s.Servers = c.Info, c.Servers
		}
	}
	if err := s.save(); err != nil {
		return
	}
	_ = os.Remove(legacy)
	_, _ = config.Update(func(c *config.Config) {
		c.VPNSubURL, c.VPNSelected = "", ""
		c.VPNActiveSub = s.ID
	})
}
