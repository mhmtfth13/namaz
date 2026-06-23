// namaz — terminalde sıradaki namaz vaktine kalan süreyi gösterir.
// Veri: namazvakti.com (Diyanet vakitleri).
// Konum: sabit (config) ya da IP geolocation (ip-api.com).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	geoURL   = "http://ip-api.com/json/?fields=status,message,city,regionName,lat,lon"
	geocode  = "https://geocoding-api.open-meteo.com/v1/search?count=1&language=tr&format=json&name=%s"
	nvURL    = "https://www.namazvakti.com/Main.php?arz=%.4f&tul=%.4f&tz_offset=%d"
	monthURL = "https://www.namazvakti.com/Monthly.php?arz=%.4f&tul=%.4f&tz_offset=%d"
	uaStr    = "Mozilla/5.0 (namaz-cli)"
)

// Gösterilecek ana 6 vakit ve ekran adları. namazvakti israk/kerahet/sabah/
// asrisani/isfirar/istibak gibi ara vakitleri de döner; onları atlıyoruz.
var mainVakits = map[string]string{
	"imsak":  "İmsak",
	"gunes":  "Güneş",
	"ogle":   "Öğle",
	"ikindi": "İkindi",
	"aksam":  "Akşam",
	"yatsi":  "Yatsı",
}

type vakit struct {
	name     string    // imsak, gunes, ...
	disp     string    // İmsak, Güneş, ...
	t        time.Time // mutlak an
	tomorrow bool      // yarına ait (yatsı sonrası eklenen İmsak)
}

type loc struct {
	Place string  `json:"place"`
	Lat   float64 `json:"lat"`
	Lon   float64 `json:"lon"`
}

// config: konum override. Mode "fixed" → Loc kullan, aksi halde IP.
type config struct {
	Mode string `json:"mode"`
	Loc  loc    `json:"loc"`
}

func main() {
	args := os.Args[1:]

	// Alt komutlar (konum yönetimi).
	if len(args) > 0 {
		switch args[0] {
		case "set":
			cmdSet(args[1:])
			return
		case "auto":
			cmdAuto()
			return
		case "where":
			cmdWhere()
			return
		}
	}

	var (
		showAll bool
		noCache bool
		plain   bool
		flagLat = math.NaN()
		flagLon = math.NaN()
	)
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-a", "--all":
			showAll = true
		case "--no-cache":
			noCache = true
		case "--plain":
			plain = true
		case "--lat":
			i++
			flagLat = mustFloat(args, i, "--lat")
		case "--lon":
			i++
			flagLon = mustFloat(args, i, "--lon")
		case "-h", "--help":
			help()
			return
		default:
			fmt.Fprintf(os.Stderr, "bilinmeyen seçenek: %s (--help)\n", a)
			os.Exit(2)
		}
	}
	if os.Getenv("NO_COLOR") != "" {
		plain = true
	}

	// Geçici koordinat flag'i: bu çalıştırma için override + kalıcı kaydet.
	if !math.IsNaN(flagLat) || !math.IsNaN(flagLon) {
		if math.IsNaN(flagLat) || math.IsNaN(flagLon) {
			fmt.Fprintln(os.Stderr, "hata: --lat ve --lon birlikte verilmeli")
			os.Exit(2)
		}
		l := loc{Place: fmt.Sprintf("%.4f, %.4f", flagLat, flagLon), Lat: flagLat, Lon: flagLon}
		saveConfig(config{Mode: "fixed", Loc: l})
	}

	place, vakits, err := load(noCache)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hata:", err)
		os.Exit(1)
	}
	render(place, vakits, showAll, newColor(plain))
}

// --- alt komutlar ---

func cmdSet(rest []string) {
	name := strings.TrimSpace(strings.Join(rest, " "))
	if name == "" {
		fmt.Fprintln(os.Stderr, "kullanım: namaz set \"şehir adı\"")
		os.Exit(2)
	}
	l, err := geocodeCity(name)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hata:", err)
		os.Exit(1)
	}
	saveConfig(config{Mode: "fixed", Loc: l})
	os.Remove(cacheFile()) // konum değişti, eski cache geçersiz
	fmt.Printf("Konum sabitlendi: %s (%.4f, %.4f)\n", l.Place, l.Lat, l.Lon)
}

func cmdAuto() {
	saveConfig(config{Mode: "auto"})
	os.Remove(cacheFile())
	fmt.Println("Konum otomatik (IP) moduna alındı.")
}

func cmdWhere() {
	c := loadConfig()
	if c.Mode == "fixed" {
		fmt.Printf("Sabit: %s (%.4f, %.4f)\n", c.Loc.Place, c.Loc.Lat, c.Loc.Lon)
	} else {
		fmt.Println("Otomatik (IP geolocation)")
	}
}

// --- konum çözümleme ---

// load: konumu çöz, bugünün cache'i varsa kullan, yoksa vakitleri çek + cache'le.
func load(noCache bool) (string, []vakit, error) {
	l, err := resolveLoc()
	if err != nil {
		return "", nil, err
	}
	today := time.Now().Format("2006-01-02")
	cf := cacheFile()

	if !noCache {
		if vs, ok := readCache(cf, today, l); ok {
			return l.Place, withTomorrow(l, vs), nil
		}
	}
	vs, err := fetchVakits(l)
	if err != nil {
		return "", nil, err
	}
	writeCache(cf, today, l, vs)
	return l.Place, withTomorrow(l, vs), nil
}

// withTomorrow: bugünün tüm vakitleri geçtiyse (yatsı–gece yarısı arası),
// yarının İmsak'ını Monthly tablosundan çekip ekler.
func withTomorrow(l loc, vs []vakit) []vakit {
	now := time.Now()
	for _, v := range vs {
		if v.t.After(now) {
			return vs // hâlâ bugün bir vakit var
		}
	}
	if t, ok := fetchTomorrowImsak(l); ok {
		vs = append(vs, vakit{name: "imsak", disp: "İmsak", t: t, tomorrow: true})
	}
	return vs
}

func resolveLoc() (loc, error) {
	if c := loadConfig(); c.Mode == "fixed" && (c.Loc.Lat != 0 || c.Loc.Lon != 0) {
		return c.Loc, nil
	}
	return fetchGeo()
}

func fetchGeo() (loc, error) {
	var g struct {
		Status, Message, City, RegionName string
		Lat, Lon                          float64
	}
	b, err := httpGet(geoURL)
	if err != nil {
		return loc{}, fmt.Errorf("konum alınamadı: %w", err)
	}
	if err := json.Unmarshal(b, &g); err != nil {
		return loc{}, fmt.Errorf("konum yanıtı çözülemedi: %w", err)
	}
	if g.Status != "success" {
		return loc{}, fmt.Errorf("konum servisi: %s", g.Message)
	}
	place := g.City
	if place == "" {
		place = fmt.Sprintf("%.2f, %.2f", g.Lat, g.Lon)
	} else if g.RegionName != "" && !strings.EqualFold(g.RegionName, g.City) {
		place = g.City + ", " + g.RegionName
	}
	return loc{Place: place, Lat: g.Lat, Lon: g.Lon}, nil
}

func geocodeCity(name string) (loc, error) {
	b, err := httpGet(fmt.Sprintf(geocode, url.QueryEscape(name)))
	if err != nil {
		return loc{}, fmt.Errorf("şehir aranamadı: %w", err)
	}
	var r struct {
		Results []struct {
			Name      string  `json:"name"`
			Admin1    string  `json:"admin1"`
			Country   string  `json:"country"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"results"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return loc{}, fmt.Errorf("arama yanıtı çözülemedi: %w", err)
	}
	if len(r.Results) == 0 {
		return loc{}, fmt.Errorf("şehir bulunamadı: %q", name)
	}
	x := r.Results[0]
	place := x.Name
	if x.Admin1 != "" && !strings.EqualFold(x.Admin1, x.Name) {
		place = x.Name + ", " + x.Admin1
	}
	return loc{Place: place, Lat: x.Latitude, Lon: x.Longitude}, nil
}

// --- vakit çekme ---

var reVakit = regexp.MustCompile(`vakitts="(\d+)"\s+data-vakitname="([a-z]+)"`)

func fetchVakits(l loc) ([]vakit, error) {
	_, off := time.Now().Zone()
	tzMin := -off / 60 // namazvakti JS getTimezoneOffset() ile aynı: dakika, doğu negatif
	b, err := httpGet(fmt.Sprintf(nvURL, l.Lat, l.Lon, tzMin))
	if err != nil {
		return nil, fmt.Errorf("vakitler alınamadı: %w", err)
	}
	ms := reVakit.FindAllStringSubmatch(string(b), -1)
	if len(ms) == 0 {
		return nil, fmt.Errorf("vakitler ayrıştırılamadı (site değişmiş olabilir)")
	}
	var out []vakit
	for _, m := range ms {
		disp, ok := mainVakits[m[2]]
		if !ok {
			continue
		}
		ts, _ := strconv.ParseInt(m[1], 10, 64)
		out = append(out, vakit{name: m[2], disp: disp, t: time.Unix(ts, 0)})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("ana vakitler bulunamadı")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].t.Before(out[j].t) })
	return out, nil
}

var reTime = regexp.MustCompile(`[0-2][0-9]:[0-5][0-9]`)

// fetchTomorrowImsak: Monthly tablosundan yarının (gün numarası) ilk saatini
// (= İmsak sütunu) ayrıştırır, yerel saat diliminde tam zaman olarak döner.
func fetchTomorrowImsak(l loc) (time.Time, bool) {
	_, off := time.Now().Zone()
	b, err := httpGet(fmt.Sprintf(monthURL, l.Lat, l.Lon, -off/60))
	if err != nil {
		return time.Time{}, false
	}
	s := string(b)
	tom := time.Now().AddDate(0, 0, 1)
	// Tablo satırı: "<td>24 Çarşamba" → ardından gelen ilk HH:MM İmsak'tır.
	i := strings.Index(s, fmt.Sprintf("<td>%d ", tom.Day()))
	if i < 0 {
		return time.Time{}, false
	}
	hm := reTime.FindString(s[i:])
	if hm == "" {
		return time.Time{}, false
	}
	h, _ := strconv.Atoi(hm[:2])
	m, _ := strconv.Atoi(hm[3:])
	return time.Date(tom.Year(), tom.Month(), tom.Day(), h, m, 0, 0, time.Local), true
}

// --- render ---

func render(place string, vs []vakit, showAll bool, c colors) {
	now := time.Now()

	var next *vakit
	for i := range vs {
		if vs[i].t.After(now) {
			next = &vs[i]
			break
		}
	}

	if next != nil {
		when := ""
		if next.tomorrow {
			when = c.dim + " (yarın)" + c.reset
		}
		// Örn: "Öğle vaktine kalan 11 dk"
		fmt.Printf("\n  %s%s%s vaktine kalan %s%s%s%s  %s%s%s\n",
			c.bold, next.disp, c.reset,
			c.accent+c.bold, humanDur(next.t.Sub(now)), c.reset, when,
			c.dim, next.t.Format("15:04"), c.reset)
		fmt.Printf("  %sLokasyon: %s%s\n", c.dim, place, c.reset)
	} else {
		fmt.Printf("\n  %sBugünün vakitleri bitti. Sırada yarın İmsak.%s\n", c.dim, c.reset)
		fmt.Printf("  %sLokasyon: %s%s\n", c.dim, place, c.reset)
	}

	if showAll || next == nil {
		fmt.Printf("\n  %s%s%s\n", c.dim, turkishDate(now), c.reset)
		printTable(vs, next, now, c)
	} else {
		fmt.Println()
	}
}

func printTable(vs []vakit, next *vakit, now time.Time, c colors) {
	// İçinde bulunduğumuz vakit = başlamış en son vakit (t <= now olan sonuncusu).
	current := -1
	for i := range vs {
		if !vs[i].t.After(now) {
			current = i
		}
	}
	for i, v := range vs {
		marker, style := "   ", c.gray // gelecek vakitler: gri
		switch {
		case i == current:
			marker, style = c.accent+" ›"+c.reset+" ", c.accent // şu anki vakit: yeşil
		case i < current:
			style = c.red // geçmiş vakitler: kırmızı
		}
		fmt.Printf("  %s%s%-7s%s %s\n", marker, style, v.disp, c.reset, v.t.Format("15:04"))
	}
	fmt.Println()
}

// --- yardımcılar ---

func mustFloat(args []string, i int, flag string) float64 {
	if i >= len(args) {
		fmt.Fprintf(os.Stderr, "hata: %s bir değer bekliyor\n", flag)
		os.Exit(2)
	}
	f, err := strconv.ParseFloat(args[i], 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hata: %s geçersiz sayı: %s\n", flag, args[i])
		os.Exit(2)
	}
	return f
}

func httpGet(u string) ([]byte, error) {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", uaStr)
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func humanDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Minute)
	h, m := int(d.Hours()), int(d.Minutes())%60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%d saat %d dk", h, m)
	case h > 0:
		return fmt.Sprintf("%d saat", h)
	default:
		return fmt.Sprintf("%d dk", m)
	}
}

var trDays = []string{"Pazar", "Pazartesi", "Salı", "Çarşamba", "Perşembe", "Cuma", "Cumartesi"}
var trMonths = []string{"", "Ocak", "Şubat", "Mart", "Nisan", "Mayıs", "Haziran",
	"Temmuz", "Ağustos", "Eylül", "Ekim", "Kasım", "Aralık"}

func turkishDate(t time.Time) string {
	return fmt.Sprintf("%d %s %s", t.Day(), trMonths[int(t.Month())], trDays[int(t.Weekday())])
}

// --- config ---

func appDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "namaz")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func configFile() string { return filepath.Join(appDir(), "config.json") }

func loadConfig() config {
	var c config
	if b, err := os.ReadFile(configFile()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func saveConfig(c config) {
	if b, err := json.MarshalIndent(c, "", "  "); err == nil {
		_ = os.WriteFile(configFile(), b, 0o644)
	}
}

// --- cache (konum + tarihe bağlı) ---

type cacheData struct {
	Date   string  `json:"date"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	Vakits []struct {
		Name string `json:"name"`
		TS   int64  `json:"ts"`
	} `json:"vakits"`
}

func cacheFile() string { return filepath.Join(appDir(), "today.json") }

func sameCoord(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func readCache(path, today string, l loc) ([]vakit, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var c cacheData
	if json.Unmarshal(b, &c) != nil || c.Date != today ||
		!sameCoord(c.Lat, l.Lat) || !sameCoord(c.Lon, l.Lon) {
		return nil, false
	}
	var vs []vakit
	for _, e := range c.Vakits {
		if disp, ok := mainVakits[e.Name]; ok {
			vs = append(vs, vakit{name: e.Name, disp: disp, t: time.Unix(e.TS, 0)})
		}
	}
	if len(vs) == 0 {
		return nil, false
	}
	sort.Slice(vs, func(i, j int) bool { return vs[i].t.Before(vs[j].t) })
	return vs, true
}

func writeCache(path, today string, l loc, vs []vakit) {
	c := cacheData{Date: today, Lat: l.Lat, Lon: l.Lon}
	for _, v := range vs {
		c.Vakits = append(c.Vakits, struct {
			Name string `json:"name"`
			TS   int64  `json:"ts"`
		}{v.name, v.t.Unix()})
	}
	if b, err := json.Marshal(c); err == nil {
		_ = os.WriteFile(path, b, 0o644)
	}
}

// --- renk ---

type colors struct{ bold, dim, accent, red, gray, reset string }

func newColor(plain bool) colors {
	if plain {
		return colors{}
	}
	return colors{
		bold:   "\x1b[1m",
		dim:    "\x1b[2m",
		accent: "\x1b[38;5;42m",  // yeşil — şu anki vakit
		red:    "\x1b[38;5;203m", // kırmızı — geçmiş vakitler
		gray:   "\x1b[38;5;245m", // gri — gelecek vakitler
		reset:  "\x1b[0m",
	}
}

func help() {
	fmt.Print(`namaz — sıradaki namaz vaktine kalan süre

Kullanım:
  namaz                  Sıradaki vakit ve kalan süre
  namaz -a               Günün tüm vakitlerini listele

Konum:
  namaz set "İstanbul"   Şehri sabitle (bir kez, kaydedilir)
  namaz --lat 41.0 --lon 29.0   Koordinatla sabitle
  namaz auto             Otomatik (IP) konuma dön
  namaz where            Mevcut konum ayarını göster

Diğer:
  namaz --plain          Renksiz çıktı (NO_COLOR da çalışır)
  namaz --no-cache       Önbelleği atla, taze çek
  namaz --help           Bu yardım

Konum varsayılan olarak IP'den otomatik bulunur (ip-api.com).
Yanlış çıkıyorsa 'namaz set' ile sabitle.
Vakitler namazvakti.com (Diyanet) kaynağından alınır.
`)
}
