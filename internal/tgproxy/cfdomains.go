package tgproxy

import (
	"context"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Домены Cloudflare-прокси из оригинала: kwsN.<домен> проксируется
// Cloudflare'ом на дата-центры Telegram. В исходниках хранятся слегка
// «зашифрованными» (сдвиг букв), расшифровка — как в config.py.
var cfEncoded = []string{
	"virkgj.com", "vmmzovy.com", "mkuosckvso.com", "zaewayzmplad.com", "twdmbzcm.com",
	"awzwsldi.com", "clngqrflngqin.com", "tjacxbqtj.com", "bxaxtxmrw.com", "dmohrsgmohcrwb.com",
	"vwbmtmoi.com", "khgrre.com", "ulihssf.com", "tmhqsdqmfpmk.com", "xwuwoqbm.com",
	"orgcnunpj.com", "zhkuldz.com", "zypoljnslxa.com", "efabnxaowuzs.com", "zaftuzsftqdq.com",
}

const cfDomainsURL = "https://raw.githubusercontent.com/Flowseal/tg-ws-proxy/main/.github/cfproxy-domains.txt"

// decodeCF — обратный сдвиг букв на их количество, ".com" → ".co.uk".
func decodeCF(s string) string {
	if !strings.HasSuffix(s, ".com") {
		return s
	}
	p := s[:len(s)-4]
	n := 0
	for _, c := range p {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			n++
		}
	}
	var b strings.Builder
	for _, c := range p {
		switch {
		case c >= 'a' && c <= 'z':
			b.WriteRune(rune((int(c-'a')-n%26+26)%26) + 'a')
		case c >= 'A' && c <= 'Z':
			b.WriteRune(rune((int(c-'A')-n%26+26)%26) + 'A')
		default:
			b.WriteRune(c)
		}
	}
	return b.String() + ".co.uk"
}

func validDomain(d string) bool {
	if len(d) == 0 || len(d) > 253 || strings.HasPrefix(d, ".") || strings.HasSuffix(d, ".") {
		return false
	}
	labels := strings.Split(d, ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, c := range l {
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
	}
	return true
}

// cfDomains — текущий список доменов Cloudflare-фолбэка и «рабочий» домен
// для каждого DC (последний сработавший пробуем первым).
type cfDomains struct {
	mu     sync.Mutex
	list   []string
	perDC  map[int]string
	client *http.Client
}

func newCFDomains() *cfDomains {
	c := &cfDomains{perDC: map[int]string{}, client: &http.Client{Timeout: 15 * time.Second}}
	var list []string
	for _, e := range cfEncoded {
		list = append(list, decodeCF(e))
	}
	c.set(list)
	return c
}

func (c *cfDomains) set(list []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.list = list
	c.perDC = map[int]string{}
}

// All — все домены (для добавления в списки обхода zapret).
func (c *cfDomains) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.list...)
}

// forDC — порядок перебора доменов для DC: последний рабочий, затем остальные вперемешку.
func (c *cfDomains) forDC(dc int) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	rest := append([]string(nil), c.list...)
	rand.Shuffle(len(rest), func(i, j int) { rest[i], rest[j] = rest[j], rest[i] })
	cur := c.perDC[dc]
	if cur == "" {
		return rest
	}
	out := []string{cur}
	for _, d := range rest {
		if d != cur {
			out = append(out, d)
		}
	}
	return out
}

func (c *cfDomains) worked(dc int, d string) {
	c.mu.Lock()
	c.perDC[dc] = d
	c.mu.Unlock()
}

// refresh подтягивает свежий список с GitHub автора (раз в час). Мусор в
// ответе отбрасывается; если годных меньше трёх — список не меняем.
func (c *cfDomains) refresh(ctx context.Context) {
	req, _ := http.NewRequestWithContext(ctx, "GET", cfDomainsURL, nil)
	req.Header.Set("User-Agent", "tg-ws-proxy")
	resp, err := c.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	seen := map[string]bool{}
	var list []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		d := strings.ToLower(decodeCF(line))
		if validDomain(d) && !seen[d] {
			seen[d] = true
			list = append(list, d)
		}
	}
	if len(list) >= 3 {
		c.set(list)
	}
}

// DefaultCFDomains — встроенный список доменов Cloudflare-фолбэка (для
// добавления в обход zapret ещё до запуска прокси).
func DefaultCFDomains() []string {
	var out []string
	for _, e := range cfEncoded {
		out = append(out, decodeCF(e))
	}
	return out
}
