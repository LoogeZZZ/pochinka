package vpn

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xtls/xray-core/common/geodata"
	"google.golang.org/protobuf/proto"

	"pochinka/internal/assets"
)

// Базы блокировок runetfreedom: облегчённые версии только с российскими
// категориями (ru-blocked, ru-available-only-inside, category-gov-ru...).
// Полный geosite.dat весит 73 МБ, эти — около 7 МБ на двоих.
type geoFile struct {
	name    string   // как назван у нас (xray ищет geosite.dat/geoip.dat)
	urls    []string // источники по очереди: GitHub Releases, jsDelivr, raw
	bundled []byte   // вшитая копия (gzip)
	need    string   // категория, без которой база бесполезна
	site    bool
}

var geoFiles = []geoFile{
	{
		name: "geosite.dat", bundled: assets.GeoSiteGz, need: "RU-BLOCKED", site: true,
		urls: []string{
			"https://github.com/runetfreedom/russia-blocked-geosite/releases/latest/download/geosite-ru-only.dat",
			"https://cdn.jsdelivr.net/gh/runetfreedom/russia-blocked-geosite@release/geosite-ru-only.dat",
			"https://raw.githubusercontent.com/runetfreedom/russia-blocked-geosite/release/geosite-ru-only.dat",
		},
	},
	{
		name: "geoip.dat", bundled: assets.GeoIPGz, need: "RU-BLOCKED-COMMUNITY",
		urls: []string{
			"https://github.com/runetfreedom/russia-blocked-geoip/releases/latest/download/geoip-ru-only.dat",
			"https://cdn.jsdelivr.net/gh/runetfreedom/russia-blocked-geoip@release/geoip-ru-only.dat",
			"https://raw.githubusercontent.com/runetfreedom/russia-blocked-geoip/release/geoip-ru-only.dat",
		},
	},
}

const geoMaxAge = 24 * time.Hour

// EnsureGeo гарантирует, что базы на месте: если их нет — распаковывает
// вшитую копию. Сеть не нужна.
func EnsureGeo(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, f := range geoFiles {
		p := filepath.Join(dir, f.name)
		if b, err := os.ReadFile(p); err == nil && validGeo(f, b) == nil {
			continue
		}
		zr, err := gzip.NewReader(bytes.NewReader(f.bundled))
		if err != nil {
			return err
		}
		b, err := io.ReadAll(zr)
		if err != nil {
			return err
		}
		if err := writeAtomic(p, b); err != nil {
			return err
		}
		// Время файла — «давно», чтобы фоновое обновление сразу взяло свежую.
		old := time.Now().Add(-48 * time.Hour)
		_ = os.Chtimes(p, old, old)
	}
	return nil
}

// UpdateGeo скачивает свежие базы, если текущие старше суток (force — всегда).
// Возвращает true, если что-то обновилось.
func UpdateGeo(ctx context.Context, dir string, force bool) (bool, error) {
	if err := EnsureGeo(dir); err != nil {
		return false, err
	}
	changed := false
	var errs []string
	for _, f := range geoFiles {
		p := filepath.Join(dir, f.name)
		if st, err := os.Stat(p); err == nil && !force && time.Since(st.ModTime()) < geoMaxAge {
			continue
		}
		b, err := fetchGeo(ctx, f)
		if err != nil {
			errs = append(errs, f.name+": "+err.Error())
			continue // остаётся прежняя база
		}
		if old, err := os.ReadFile(p); err == nil && bytes.Equal(old, b) {
			now := time.Now()
			_ = os.Chtimes(p, now, now)
			continue
		}
		if err := writeAtomic(p, b); err != nil {
			return changed, err
		}
		changed = true
	}
	if len(errs) > 0 {
		return changed, fmt.Errorf("база блокировок не обновилась (%s)", strings.Join(errs, "; "))
	}
	return changed, nil
}

// fetchGeo пробует источники по очереди, по две попытки на каждый.
func fetchGeo(ctx context.Context, f geoFile) ([]byte, error) {
	var lastErr error
	for _, u := range f.urls {
		for attempt := 0; attempt < 2; attempt++ {
			b, err := download(ctx, u)
			if err == nil {
				err = validGeo(f, b)
			}
			if err == nil {
				return b, nil
			}
			lastErr = err
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 2 * time.Second):
			}
		}
	}
	return nil, lastErr
}

// validGeo проверяет, что это действительно база xray с нужной категорией,
// а не страница ошибки или обрывок файла.
func validGeo(f geoFile, b []byte) error {
	if len(b) < 1024 {
		return errors.New("файл слишком маленький")
	}
	if f.site {
		var l geodata.GeoSiteList
		if err := proto.Unmarshal(b, &l); err != nil {
			return errors.New("повреждённая база")
		}
		for _, e := range l.Entry {
			if strings.EqualFold(e.Code, f.need) && len(e.Domain) > 0 {
				return nil
			}
		}
	} else {
		var l geodata.GeoIPList
		if err := proto.Unmarshal(b, &l); err != nil {
			return errors.New("повреждённая база")
		}
		for _, e := range l.Entry {
			if strings.EqualFold(e.Code, f.need) && len(e.Cidr) > 0 {
				return nil
			}
		}
	}
	return fmt.Errorf("в базе нет категории %s", strings.ToLower(f.need))
}

func download(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", "Pochinka")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 100<<20))
}

// writeAtomic: временный файл со случайным именем (его нельзя подложить
// заранее) и переименование поверх.
func writeAtomic(p string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(p), filepath.Base(p)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, p)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}
