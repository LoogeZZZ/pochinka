package zapret

// Игровые домены из обсуждений zapret-discord-youtube. В релиз их не берут,
// поэтому приложение само дописывает их в *-user.txt (поддомены zapret
// учитывает автоматически, так что достаточно корневого домена).
//
// Важно: в zapret исключение всегда побеждает включение. Поэтому для игр,
// которые обход ЛОМАЕТ (Steam, HoYoverse, Gryphline, EA), домены идут в
// исключения, а не в обход.

// GameGroup — набор правил для одной игры/издателя. Включается тумблером.
type GameGroup struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Games string `json:"games"` // подпись для интерфейса
	// DefaultOff — группа выключена, пока пользователь сам её не включит.
	DefaultOff bool `json:"defaultOff"`

	Include   []string `json:"-"` // домены через обход
	Exclude   []string `json:"-"` // домены не трогать
	IPExclude []string `json:"-"` // подсети не трогать (важно при «Портах для игр»)
	// Unexclude — убрать эти домены из исключений самого релиза
	// (lists/list-exclude.txt), иначе Include для них не сработает.
	Unexclude []string `json:"-"`
}

var GameGroups = []GameGroup{
	{
		// #14165
		ID: "photon", Name: "Photon", Games: "R.E.P.O., PEAK, Phasmophobia, VRChat",
		Include: []string{"photonengine.io", "photonmaster.online-fix.me"},
	},
	{
		// #14165
		ID: "gd", Name: "Geometry Dash", Games: "сервера, Globed, Geode, музыка",
		Include: []string{"boomlings.com", "geometrydash.com", "robtopgames.com", "geode-sdk.org", "newgrounds.com", "ncs.io"},
	},
	{
		// #14165. Из списка в issue выброшены общие CDN (amazonaws,
		// akamaized, cloudfront...), speedtest, OCSP и поддельные rbxcdn.* —
		// иначе обход ломает половину интернета.
		ID: "roblox", Name: "Roblox", Games: "клиент, Studio, сайт",
		Include: []string{"roblox.com", "rbxcdn.com", "rblx.com", "robloxapp.com", "robloxgames.com", "robloxlabs.com", "rbx.com", "rbxlabs.com"},
	},
	{
		// Valorant чаще всего ломает сам обход: VAN 68/185, VAL 43, «сторонняя
		// программа», скачки пинга (#17515, #9718, #8242, #17716, #17751).
		// Подсети серверов Riot (#9718) исключаем, чтобы «Порты для игр»
		// никогда не трогали матч. Домены Riot релиз уже исключает.
		ID: "valorant", Name: "Valorant / LoL", Games: "не трогать матчи Riot — меньше ошибок Vanguard",
		IPExclude: []string{
			"104.160.128.0/19", "162.249.72.0/21", "192.64.168.0/21", "185.40.64.0/22",
			"43.229.64.0/22", "45.7.36.0/22", "103.219.128.0/22", "103.240.224.0/22", "45.250.208.0/22",
		},
	},
	{
		// У части провайдеров (МТС и др.) реально блокируют вход в Riot Client
		// (#16702, #18006). Помогает только снять исключения релиза и пустить
		// домены Riot через обход. Выключено по умолчанию: остальным это вредит.
		ID: "riot-login", Name: "Riot: не входит в аккаунт", DefaultOff: true,
		Games:     "включать, только если Riot Client висит на входе",
		Unexclude: []string{"riotgames.com", "riotcdn.net", "playvalorant.com", "leagueoflegends.com"},
		Include:   []string{"riotgames.com", "riotcdn.net", "riotcdn.com", "rstatic.net", "pvp.net", "rgpub.io", "playvalorant.com"},
	},
	{
		// Steam в РФ работает напрямую, ломает его сам обход: пустые картинки
		// магазина, вечный вход, медленный запуск (#16728, #17398, #17573,
		// #17822, #17828). Flowseal отказался добавлять Steam в обход (#7381).
		ID: "steam", Name: "Steam", Games: "магазин, вход, загрузки — без обхода",
		Exclude: []string{"steamstatic.com", "steamcontent.com", "api.steampowered.com"},
	},
	{
		// #8465, #13532, #16548, #17576: обход ломает вход и сервера HoYo.
		ID: "hoyo", Name: "HoYoverse", Games: "Genshin, Star Rail, ZZZ, Honkai 3rd — без обхода",
		Exclude:   []string{"hoyoverse.com", "hoyolab.com", "mihoyo.com", "yuanshen.com", "zenlesszonezero.com", "starrails.com", "hoyo.link"},
		IPExclude: []string{"47.100.0.0/15", "47.102.0.0/15", "47.116.0.0/17"},
	},
	{
		// #9364, #10790, #11903, #16730: «через list-general вообще не работает».
		ID: "gryphline", Name: "Arknights: Endfield", Games: "Gryphline / Hypergryph — без обхода",
		Exclude: []string{"gryphline.com", "hypergryph.com", "hg-cdn.com", "hg-static.com", "skport.com", "helpshift.com", "anticheatexpert.com", "wetest.net"},
	},
	{
		// #11328: заработало только после переноса из исключений в обход.
		ID: "yostar", Name: "Arknights", Games: "глобальная версия (Yostar)",
		Include: []string{"arknights.global", "yo-star.com", "yostarplat.com"},
	},
	{
		// #12529, #12819, #12482: античит и вход ломаются от обхода.
		ID: "nte", Name: "Neverness to Everness", Games: "Perfect World — без обхода",
		Exclude: []string{"perfectworld.com", "perfectworldgames.com", "pwsdk.com", "pwgam.es", "wanmei.com", "hotta.studio", "anticheatexpert.com"},
	},
	{
		// #1930. Основная проблема WuWa — блок по IP: если не помогло,
		// включите «Порты для игр» (TCP+UDP), так советуют в #6519.
		ID: "kuro", Name: "Wuthering Waves", Games: "Kuro Games; не помогло — включите порты для игр",
		Include: []string{"kurogame-service.com"},
	},
	{
		// #6339, #4204, #10279.
		ID: "bluearchive", Name: "Blue Archive", Games: "Nexon",
		Include: []string{"nexon.com", "bluearchive.com", "bluearchive.jp"},
	},
	{
		// #14786
		ID: "ea", Name: "EA", Games: "EA App, Battlefield — без обхода",
		Exclude: []string{"ea.com", "cloudsync-prod.s3.us-east-1.amazonaws.com"},
	},
}

// GameRules — итог по всем включённым группам.
type GameRules struct {
	Include, Exclude, IPExclude, Unexclude []string
}

// IsGameEnabled учитывает и явный выбор пользователя, и умолчание группы.
func IsGameEnabled(g GameGroup, disabled, enabled []string) bool {
	for _, id := range disabled {
		if id == g.ID {
			return false
		}
	}
	for _, id := range enabled {
		if id == g.ID {
			return true
		}
	}
	return !g.DefaultOff
}

// CollectGameRules собирает правила всех включённых групп.
func CollectGameRules(disabled, enabled []string) GameRules {
	var r GameRules
	for _, g := range GameGroups {
		if !IsGameEnabled(g, disabled, enabled) {
			continue
		}
		r.Include = append(r.Include, g.Include...)
		r.Exclude = append(r.Exclude, g.Exclude...)
		r.IPExclude = append(r.IPExclude, g.IPExclude...)
		r.Unexclude = append(r.Unexclude, g.Unexclude...)
	}
	return r
}
