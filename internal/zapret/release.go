package zapret

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const repo = "Flowseal/zapret-discord-youtube"

var httpClient = &http.Client{Timeout: 60 * time.Second}

type Release struct {
	Version string
	ZipURL  string
}

// LatestRelease узнаёт последнюю версию. Сначала через API, а если он не
// отвечает (лимит 60 запросов/час без токена), то по редиректу
// /releases/latest — у него лимита нет, а имя архива предсказуемое.
func LatestRelease(ctx context.Context) (Release, error) {
	r, err := latestViaAPI(ctx)
	if err == nil {
		return r, nil
	}
	r2, err2 := latestViaRedirect(ctx)
	if err2 == nil {
		return r2, nil
	}
	return Release{}, fmt.Errorf("GitHub недоступен: %v; %v", err, err2)
}

func latestViaAPI(ctx context.Context) (Release, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Pochinka")
	resp, err := httpClient.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Release{}, fmt.Errorf("api: HTTP %d", resp.StatusCode)
	}
	var body struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Release{}, err
	}
	for _, a := range body.Assets {
		if strings.HasSuffix(strings.ToLower(a.Name), ".zip") {
			return Release{Version: body.TagName, ZipURL: a.URL}, nil
		}
	}
	return Release{}, errors.New("в релизе нет zip-архива")
}

func latestViaRedirect(ctx context.Context) (Release, error) {
	c := *httpClient
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, _ := http.NewRequestWithContext(ctx, "HEAD", "https://github.com/"+repo+"/releases/latest", nil)
	resp, err := c.Do(req)
	if err != nil {
		return Release{}, err
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	i := strings.LastIndex(loc, "/tag/")
	if i < 0 {
		return Release{}, errors.New("неожиданный ответ GitHub")
	}
	tag := loc[i+len("/tag/"):]
	return Release{
		Version: tag,
		ZipURL:  fmt.Sprintf("https://github.com/%s/releases/download/%s/zapret-discord-youtube-%s.zip", repo, tag, tag),
	}, nil
}

var versionRe = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._-]{0,40}$`)

// ValidVersion — имя версии безопасно как имя папки (никаких "..", путей).
func ValidVersion(v string) bool { return versionRe.MatchString(v) && !strings.Contains(v, "..") }

// Install скачивает и распаковывает релиз в versionsDir/<версия>.
// Распаковка идёт во временную папку и в конце переименовывается, поэтому
// недокачанная версия никогда не станет "установленной".
func Install(ctx context.Context, versionsDir string, r Release) (string, error) {
	if !ValidVersion(r.Version) {
		return "", fmt.Errorf("странная версия %q", r.Version)
	}
	final := filepath.Join(versionsDir, r.Version)
	if IsInstalled(final) {
		return final, nil
	}

	tmpZip, err := os.CreateTemp(versionsDir, "download-*.zip")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpZip.Name())

	req, _ := http.NewRequestWithContext(ctx, "GET", r.ZipURL, nil)
	req.Header.Set("User-Agent", "Pochinka")
	dl := &http.Client{Timeout: 5 * time.Minute}
	resp, err := dl.Do(req)
	if err != nil {
		tmpZip.Close()
		return "", err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		tmpZip.Close()
		return "", fmt.Errorf("скачивание: HTTP %d", resp.StatusCode)
	}
	_, err = io.Copy(tmpZip, io.LimitReader(resp.Body, 200<<20))
	resp.Body.Close()
	tmpZip.Close()
	if err != nil {
		return "", err
	}

	staging := final + ".partial"
	_ = os.RemoveAll(staging)
	if err := unzipStrip(tmpZip.Name(), staging); err != nil {
		os.RemoveAll(staging)
		return "", err
	}
	if !IsInstalled(staging) {
		os.RemoveAll(staging)
		return "", errors.New(`файлы zapret (winws.exe, WinDivert) удалил антивирус — добавьте папку C:\ProgramData\Pochinka\zapret в исключения Защитника и нажмите ещё раз`)
	}
	_ = os.RemoveAll(final)
	if err := os.Rename(staging, final); err != nil {
		return "", err
	}
	return final, nil
}

// IsInstalled — есть ли в папке всё нужное для запуска.
func IsInstalled(dir string) bool {
	for _, f := range []string{`bin\winws.exe`, `bin\WinDivert.dll`, `bin\WinDivert64.sys`, `bin\cygwin1.dll`} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			return false
		}
	}
	return true
}

// unzipStrip распаковывает архив, срезая общую корневую папку
// (в релизе всё лежит в zapret-discord-youtube-X.Y.Z/).
func unzipStrip(zipPath, dst string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	prefix := commonRoot(zr.File)
	for _, f := range zr.File {
		name := strings.TrimPrefix(f.Name, prefix)
		if name == "" {
			continue
		}
		// В zip разделитель — "/", но Windows понимает и "\": приводим к
		// одному виду до проверки, иначе "a\..\..\x.dll" прошёл бы мимо.
		name = strings.ReplaceAll(name, `\`, "/")
		clean := path.Clean("/" + name)[1:] // защита от ../ в архиве
		if clean == "" || strings.HasPrefix(clean, "..") || strings.Contains(clean, ":") {
			continue
		}
		target := filepath.Join(dst, filepath.FromSlash(clean))
		if rel, err := filepath.Rel(dst, target); err != nil || rel == ".." || strings.HasPrefix(rel, `..\`) || filepath.IsAbs(rel) {
			continue
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := extractFile(f, target); err != nil {
			return err
		}
	}
	return nil
}

func commonRoot(files []*zip.File) string {
	root := ""
	for _, f := range files {
		i := strings.Index(f.Name, "/")
		if i < 0 {
			return ""
		}
		top := f.Name[:i+1]
		if root == "" {
			root = top
		} else if root != top {
			return ""
		}
	}
	return root
}

func extractFile(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, io.LimitReader(rc, 100<<20))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// InstalledVersions — список распакованных версий (по имени папки).
func InstalledVersions(versionsDir string) []string {
	entries, _ := os.ReadDir(versionsDir)
	var out []string
	for _, e := range entries {
		if e.IsDir() && ValidVersion(e.Name()) && IsInstalled(filepath.Join(versionsDir, e.Name())) {
			out = append(out, e.Name())
		}
	}
	return out
}

// CompareVersions сравнивает "1.10.3" и "1.10.10" по числам. >0 если a новее.
func CompareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(strings.TrimLeft(pa[i], "v"))
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(strings.TrimLeft(pb[i], "v"))
		}
		if x != y {
			return x - y
		}
	}
	return 0
}
