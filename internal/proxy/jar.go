package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"mcos/internal/server/providers"
)

// Velocity, PaperMC'nin fill v3 API'sinden gelir (Paper ile aynı sunucu,
// bkz. providers/paper.go).
const fillAPI = "https://fill.papermc.io/v3/projects/velocity"

// OfflineURL, imaja gömülen sabit Velocity sürümüdür.
//
// offline-manifest.txt'deki satırla AYNI olmak ZORUNDA: depo dosya adı URL'in
// özetinden türetilir (providers.CachedFile). İnternetsiz bir MCOS'ta ortak
// dünya proxy'si bu jar'la açılır. 4.2.0 Java 25 ister; Java 25 JRE de
// çevrimdışı pakette (java25-jre).
const (
	OfflineURL     = "https://fill-data.papermc.io/v1/objects/35a5596a5468a035d8a32c8de5ebb0dc6b8d8f0cc3ff5169d514aca762af8aa8/velocity-4.2.0-30.jar"
	offlineVersion = "4.2.0"
	offlineBuild   = 30
	offlineJava    = 25
)

// metaName, indirilen jar'ın sürüm bilgisini tutan dosya (depoda).
const metaName = "velocity.json"

// Jar is a usable Velocity build.
type Jar struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Build   int    `json:"build"`
	// Java, bu sürümün istediği en düşük Java ana sürümüdür (API'den).
	Java int `json:"java"`
}

// Major is Velocity's major version (yapılandırma biçimini seçer).
func (j Jar) Major() int {
	n, _ := strconv.Atoi(strings.SplitN(j.Version, ".", 2)[0])
	return n
}

// Cached returns a Velocity jar available WITHOUT the network.
//
// Önce daha önce indirilen (depodaki velocity.json), sonra imajla gelen
// sabit sürüm. Ağ gerekmez: yeniden uzlaştırma döngüsü bunu her turda çağırır.
func Cached() (Jar, bool) {
	if j, ok := downloaded(); ok {
		return j, true
	}
	if p, ok := providers.CachedFile(OfflineURL); ok {
		return Jar{Path: p, Version: offlineVersion, Build: offlineBuild, Java: offlineJava}, true
	}
	return Jar{}, false
}

// downloaded returns the jar Fetch saved earlier.
func downloaded() (Jar, bool) {
	if b, err := os.ReadFile(filepath.Join(providers.CacheDir, metaName)); err == nil {
		var j Jar
		if json.Unmarshal(b, &j) == nil && j.Path != "" {
			if fi, err := os.Stat(j.Path); err == nil && fi.Size() > 0 {
				if j.Java <= 0 {
					j.Java = 21
				}
				return j, true
			}
		}
	}
	return Jar{}, false
}

// Resolve returns a Velocity jar: daha önce indirilen, yoksa en yeni kararlı
// sürüm (ağdan), o da olmazsa imajla gelen sabit sürüm.
//
// İmajdaki sürüm yalnızca SON ÇARE: yeni Minecraft sürümleri yeni Velocity
// ister; internet varken eski gömülü jar'a takılı kalmak, bir gün yeni
// sürümdeki oyuncuların proxy'den geçememesi demekti.
func Resolve(ctx context.Context) (Jar, error) {
	if j, ok := downloaded(); ok {
		return j, nil
	}
	j, err := Fetch(ctx, &http.Client{Timeout: 3 * time.Minute})
	if err == nil {
		return j, nil
	}
	if p, ok := providers.CachedFile(OfflineURL); ok {
		return Jar{Path: p, Version: offlineVersion, Build: offlineBuild, Java: offlineJava}, nil
	}
	return Jar{}, err
}

type fillVersions struct {
	Versions []struct {
		Version struct {
			ID      string `json:"id"`
			Support struct {
				Status string `json:"status"`
			} `json:"support"`
			Java struct {
				Version struct {
					Minimum int `json:"minimum"`
				} `json:"version"`
			} `json:"java"`
		} `json:"version"`
	} `json:"versions"`
}

type fillBuild struct {
	ID        int    `json:"id"`
	Channel   string `json:"channel"`
	Downloads map[string]struct {
		Name      string `json:"name"`
		URL       string `json:"url"`
		Checksums struct {
			SHA256 string `json:"sha256"`
		} `json:"checksums"`
	} `json:"downloads"`
}

// Fetch downloads the newest stable Velocity into the artifact store.
//
// "Kararlı" = SNAPSHOT olmayan ve API'nin SUPPORTED dediği en yeni sürüm,
// onun da STABLE/RECOMMENDED en yeni derlemesi. SNAPSHOT'lar ağ geçidinde
// deneysel yazılım demek; bir oyuncu ağının tek kapısı olan yazılım
// deneysel olmamalı.
func Fetch(ctx context.Context, client *http.Client) (Jar, error) {
	var vs fillVersions
	if err := getJSON(ctx, client, fillAPI+"/versions", &vs); err != nil {
		return Jar{}, fmt.Errorf("Velocity sürüm listesi alınamadı: %w", err)
	}
	type cand struct {
		id        string
		java      int
		supported bool
	}
	var cs []cand
	for _, v := range vs.Versions {
		id := v.Version.ID
		if id == "" || strings.Contains(strings.ToUpper(id), "SNAPSHOT") {
			continue
		}
		cs = append(cs, cand{id, v.Version.Java.Version.Minimum,
			strings.EqualFold(v.Version.Support.Status, "SUPPORTED")})
	}
	if len(cs) == 0 {
		return Jar{}, fmt.Errorf("Velocity: kararlı sürüm bulunamadı")
	}
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].supported != cs[j].supported {
			return cs[i].supported
		}
		return newerVersion(cs[i].id, cs[j].id)
	})
	pick := cs[0]

	var builds []fillBuild
	if err := getJSON(ctx, client, fmt.Sprintf("%s/versions/%s/builds", fillAPI, pick.id), &builds); err != nil {
		return Jar{}, fmt.Errorf("Velocity %s derlemeleri alınamadı: %w", pick.id, err)
	}
	if len(builds) == 0 {
		return Jar{}, fmt.Errorf("Velocity %s için derleme yok", pick.id)
	}
	sort.Slice(builds, func(i, j int) bool { return builds[i].ID > builds[j].ID })
	b := builds[0]
	for _, x := range builds {
		if x.Channel == "STABLE" || x.Channel == "RECOMMENDED" {
			b = x
			break
		}
	}
	dl, ok := b.Downloads["server:default"]
	if !ok || dl.URL == "" {
		return Jar{}, fmt.Errorf("Velocity %s-%d için indirme bağlantısı yok", pick.id, b.ID)
	}
	name := dl.Name
	if name == "" {
		name = fmt.Sprintf("velocity-%s-%d.jar", pick.id, b.ID)
	}
	dst := filepath.Join(providers.CacheDir, filepath.Base(name))
	if err := download(ctx, client, dl.URL, dst, dl.Checksums.SHA256); err != nil {
		return Jar{}, err
	}
	j := Jar{Path: dst, Version: pick.id, Build: b.ID, Java: pick.java}
	if j.Java <= 0 {
		j.Java = 21
	}
	meta, _ := json.MarshalIndent(j, "", "  ")
	_ = os.WriteFile(filepath.Join(providers.CacheDir, metaName), meta, 0o644)
	return j, nil
}

// newerVersion compares dotted versions numerically ("4.10.0" > "4.9.1").
func newerVersion(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func getJSON(ctx context.Context, client *http.Client, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "mcos/0.1")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// download writes url to dst atomically, checking the SHA-256 if known.
//
// Yarım kalmış bir jar "depoda var" sanılıp her açılışta bozuk Velocity
// başlatırdı: önce geçici ada yazılır, özet tutarsa taşınır.
func download(ctx context.Context, client *http.Client, url, dst, sum string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "mcos/0.1")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Velocity indirilemedi: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Velocity indirilemedi: %s", resp.Status)
	}
	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("Velocity indirilemedi: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if sum != "" && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), sum) {
		_ = os.Remove(tmp)
		return fmt.Errorf("Velocity indirmesi bozuk (SHA-256 tutmuyor)")
	}
	return os.Rename(tmp, dst)
}
