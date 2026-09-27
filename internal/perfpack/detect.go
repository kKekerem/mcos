package perfpack

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// scanMods returns mod id -> jar file names for every jar in dir.
//
// ── Neden dosya adına değil kimliğe bakılıyor ───────────────────────────────
// Aynı mod çok farklı adlarla gelir: Modrinth "lithium-fabric-0.21.4+mc1.21.11.jar",
// CurseForge "lithium-fabric-mc1.21.11-0.21.4.jar", kullanıcının USB'si
// "Lithium.jar". Adla eşleştirme bunlardan birini kaçırır ve mod İKİNCİ kez
// kurulurdu; Fabric iki kopyayı görünce "duplicate mod" diye açılışı
// düşürür. Jar'ın içindeki kimlik (fabric.mod.json "id", mods.toml "modId")
// her yerde aynıdır.
//
// Kimliği okunamayan jar (bozuk, meta verisiz) burada görünmez; paketin
// indireceği dosyayla AYNI ADLI bir dosya installMod'da ayrıca denetlenir.
//
// Bir kimliğe BİRDEN ÇOK dosya düşebilir (paketin eski kopyası + kullanıcının
// sonradan eklediği kopya); hepsi döner ki kullanıcınınki görülsün.
func scanMods(dir string) map[string][]string {
	out := map[string][]string{}
	es, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range es {
		name := e.Name()
		// Yalnızca *.jar: sunucu ".jar.disabled" ya da indirme artığı
		// ".part" dosyalarını yüklemez, onlar "kurulu" sayılmamalı.
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(name), ".jar") {
			continue
		}
		for _, id := range jarModIDs(filepath.Join(dir, name)) {
			if !slices.Contains(out[id], name) {
				out[id] = append(out[id], name)
			}
		}
	}
	return out
}

// jarModIDs reads the mod ids a jar declares (and the ids it "provides").
func jarModIDs(path string) []string {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil
	}
	defer z.Close()
	var ids []string
	for _, f := range z.File {
		switch f.Name {
		case "fabric.mod.json":
			ids = append(ids, fabricIDs(f)...)
		case "quilt.mod.json":
			ids = append(ids, quiltIDs(f)...)
		case "META-INF/neoforge.mods.toml", "META-INF/mods.toml":
			ids = append(ids, tomlModIDs(f)...)
		}
	}
	for i := range ids {
		ids[i] = strings.ToLower(strings.TrimSpace(ids[i]))
	}
	return ids
}

func readZip(f *zip.File) []byte {
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	// Meta veri dosyaları küçüktür; bozuk/kötü niyetli bir jar'ın belleği
	// doldurmasına izin verilmez.
	b, _ := io.ReadAll(io.LimitReader(rc, 1<<20))
	return b
}

func fabricIDs(f *zip.File) []string {
	var m struct {
		ID       string   `json:"id"`
		Provides []string `json:"provides"`
	}
	if json.Unmarshal(readZip(f), &m) != nil || m.ID == "" {
		return nil
	}
	return append([]string{m.ID}, m.Provides...)
}

func quiltIDs(f *zip.File) []string {
	var m struct {
		Loader struct {
			ID string `json:"id"`
		} `json:"quilt_loader"`
	}
	if json.Unmarshal(readZip(f), &m) != nil || m.Loader.ID == "" {
		return nil
	}
	return []string{m.Loader.ID}
}

// tomlModIDs returns the modId values of [[mods]] tables only.
//
// [[dependencies.x]] tablolarında da "modId" anahtarı var (ör. modId =
// "neoforge"); onları saymak her NeoForge modunu "neoforge kurulu" diye
// işaretlerdi. Bu yüzden hangi tablonun içinde olunduğu izlenir.
func tomlModIDs(f *zip.File) []string {
	var ids []string
	inMods := false
	sc := bufio.NewScanner(strings.NewReader(string(readZip(f))))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			inMods = line == "[[mods]]"
			continue
		}
		if !inMods {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) != "modId" {
			continue
		}
		v = strings.TrimSpace(v)
		if i := strings.Index(v, "#"); i > 0 {
			v = strings.TrimSpace(v[:i])
		}
		v = strings.Trim(v, `"'`)
		if v != "" {
			ids = append(ids, v)
		}
	}
	return ids
}
