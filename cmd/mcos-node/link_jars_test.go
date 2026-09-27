package main

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// DÜĞÜM: SÜRÜME GÖRE ORTAK DÜNYA JAR'I, PROGRAMIN KLASÖRÜNDEN
// ════════════════════════════════════════════════════════════════════════════
//
// Düğüm paketi eskiden yalnızca 1.21.11 için derlenmiş mcos-link.jar'ı
// taşıyordu ve düğüm sürüm sormadan onu HER Fabric sunucusuna kopyalıyordu.
// MCOS kutusu 26.3 ortak dünyası gönderdiğinde bu PC'deki yarı ya modsuz
// kalır ya da açılışta düşerdi. Seçim artık MCOS kutusuyla AYNI kural
// (internal/linkjar) ve jar'lar programın yanındaki mods/link'te.

// writeNodeJar writes a real zip; tag makes jars distinguishable.
func writeNodeJar(t *testing.T, path, id, version, tag string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	if id != "" {
		w, _ := zw.Create("fabric.mod.json")
		_, _ = w.Write([]byte(`{"id":"` + id + `","version":"` + version + `"}`))
	}
	w, _ := zw.Create("tag.txt")
	_, _ = w.Write([]byte(tag))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// fakeProgramDir builds <program>/mods/link with an index and the jars it
// names, and points linkLocator at that program folder. Returns the folder.
func fakeProgramDir(t *testing.T, lines ...string) string {
	t.Helper()
	base := t.TempDir()
	link := filepath.Join(base, "mods", "link")
	byLoader := map[string][]string{}
	for _, l := range lines {
		f := strings.Split(l, "\t")
		byLoader[f[0]] = append(byLoader[f[0]], l)
		writeNodeJar(t, filepath.Join(link, f[2]), "mcos-link", "1.0.1", f[2])
		if len(f) > 3 && f[3] != "-" {
			v := strings.TrimSuffix(strings.TrimPrefix(f[3], "fabric-api-"), ".jar")
			writeNodeJar(t, filepath.Join(link, f[3]), "fabric-api", v, f[3])
		}
	}
	for loader, ls := range byLoader {
		p := filepath.Join(link, "index-"+loader+".tsv")
		if err := os.WriteFile(p, []byte("# sınama\n"+strings.Join(ls, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := linkBaseDir
	linkBaseDir = func() string { return base }
	t.Cleanup(func() { linkBaseDir = old })
	// Depo kökünden koşan bir sınama ./dist/mods/link'i görmesin.
	t.Chdir(t.TempDir())
	return base
}

var nodeIndex = []string{
	"fabric\t1.20.5\tmcos-link-fabric-1.20.5-1.21.10.jar\tfabric-api-0.97.8+1.20.5.jar",
	"fabric\t1.21.11\tmcos-link-fabric-1.21.11.jar\tfabric-api-0.141.6+1.21.11.jar",
	"fabric\t26.3\tmcos-link-fabric-26.1-26.3.jar\tfabric-api-0.161.0+26.3.jar",
	"paper\t1.21.1\tmcos-link-paper.jar\t-",
	"paper\t26.3+\tmcos-link-paper.jar\t-",
}

func sameBytes(a, b string) bool {
	x, err1 := os.ReadFile(a)
	y, err2 := os.ReadFile(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}

// 26.3 Fabric: programın klasöründeki 26.3 jar'ı ve 26.3'ün fabric-api'si
// kurulur; ağa çıkılmaz; eski sürümün fabric-api'si kaldırılır.
func TestNodeFabric263FromProgramFolder(t *testing.T) {
	base := fakeProgramDir(t, nodeIndex...)
	h, _ := newTestHost(t)
	h.fabricAPI = func(*model.Server, string) (string, error) {
		t.Error("fabric-api paketteyken indirme denendi")
		return "", errors.New("ağ yok")
	}
	data := t.TempDir()
	old := filepath.Join(data, "mods", "fabric-api-0.141.6+1.21.11.jar")
	writeNodeJar(t, old, "fabric-api", "0.141.6+1.21.11", "eski")
	srv := &model.Server{ID: "n263", Name: "O", DataDir: data, Software: model.SoftwareFabric, MCVersion: "26.3"}
	if err := h.installLinkMod(srv); err != nil {
		t.Fatalf("26.3: %v", err)
	}
	link := filepath.Join(base, "mods", "link")
	if !sameBytes(filepath.Join(data, "mods", linkModName), filepath.Join(link, "mcos-link-fabric-26.1-26.3.jar")) {
		t.Error("mods/mcos-link.jar 26.3 yapısı değil")
	}
	if !sameBytes(filepath.Join(data, "mods", "fabric-api-0.161.0+26.3.jar"), filepath.Join(link, "fabric-api-0.161.0+26.3.jar")) {
		t.Error("26.3'ün fabric-api'si paketten kopyalanmadı")
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("1.21.11'in fabric-api'si kaldı — 26.3 sunucusu açılmaz")
	}
	// Paper 26.4: "+" satırı.
	pdata := t.TempDir()
	psrv := &model.Server{ID: "p", Name: "P", DataDir: pdata, Software: model.SoftwarePaper, MCVersion: "26.4"}
	if err := h.installLinkMod(psrv); err != nil {
		t.Fatalf("Paper 26.4: %v", err)
	}
	if !sameBytes(filepath.Join(pdata, "plugins", linkPluginName), filepath.Join(link, "mcos-link-paper.jar")) {
		t.Error("plugins/mcos-link-paper.jar kurulmadı")
	}
}

// Desteklenmeyen sürüm: MCOS kutusuyla aynı neden; eski kopya kaldırılır.
func TestNodeUnsupportedVersion(t *testing.T) {
	fakeProgramDir(t, nodeIndex...)
	h, _ := newTestHost(t)
	data := t.TempDir()
	stale := filepath.Join(data, "mods", linkModName)
	writeNodeJar(t, stale, "mcos-link", "1.0.1", "eski")
	srv := &model.Server{ID: "u", Name: "U", DataDir: data, Software: model.SoftwareFabric, MCVersion: "1.20.1"}
	err := h.installLinkMod(srv)
	if err == nil || err.Error() != "Fabric 1.20.1 için ortak dünya modu yok (desteklenen: 1.20.5–26.3)" {
		t.Errorf("hata: %v", err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("uyumsuz eski mod kaldırılmadı — sunucu açılışta düşer")
	}
	if err := h.installLinkMod(&model.Server{ID: "f", DataDir: data, Software: model.SoftwareForge, MCVersion: "1.21.1"}); err == nil ||
		err.Error() != "Forge ortak dünyayı desteklemiyor; Fabric veya Paper seçin" {
		t.Errorf("Forge: %v", err)
	}
}

// Eski paket: programın yanında yalnızca indekssiz mcos-link.jar. YALNIZCA
// 1.21.11'e kurulur.
func TestNodeLegacyJarNextToProgram(t *testing.T) {
	base := t.TempDir()
	writeNodeJar(t, filepath.Join(base, linkModName), "mcos-link", "1.0.1", "eski")
	writeNodeJar(t, filepath.Join(base, "fabric-api-0.141.6+1.21.11.jar"), "fabric-api", "0.141.6+1.21.11", "api")
	old := linkBaseDir
	linkBaseDir = func() string { return base }
	t.Cleanup(func() { linkBaseDir = old })
	t.Chdir(t.TempDir())
	h, _ := newTestHost(t)

	data := t.TempDir()
	if err := h.installLinkMod(&model.Server{ID: "l", DataDir: data, Software: model.SoftwareFabric, MCVersion: "1.21.11"}); err != nil {
		t.Fatalf("1.21.11: %v", err)
	}
	if _, err := os.Stat(filepath.Join(data, "mods", "fabric-api-0.141.6+1.21.11.jar")); err != nil {
		t.Error("programın yanındaki fabric-api kullanılmadı")
	}
	if err := h.installLinkMod(&model.Server{ID: "m", DataDir: t.TempDir(), Software: model.SoftwareFabric, MCVersion: "26.3"}); err == nil ||
		!strings.Contains(err.Error(), "desteklenen: 1.21.11") {
		t.Errorf("eski jar 26.3'e kuruldu ya da neden yok: %v", err)
	}
}

// Windows kendini program klasörüne kopyalarken mods/link'i de almalı.
func TestCopyLinkJars(t *testing.T) {
	fakeProgramDir(t, nodeIndex...)
	dst := t.TempDir()
	n, err := copyLinkJars(dst)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"index-fabric.tsv", "index-paper.tsv",
		"mcos-link-fabric-26.1-26.3.jar", "fabric-api-0.161.0+26.3.jar", "mcos-link-paper.jar"} {
		if _, err := os.Stat(filepath.Join(dst, "mods", "link", f)); err != nil {
			t.Errorf("%s kopyalanmadı (%d dosya)", f, n)
		}
	}
	// İndeks yoksa HATA (sessizce boş klasör değil).
	old := linkBaseDir
	linkBaseDir = func() string { return t.TempDir() }
	defer func() { linkBaseDir = old }()
	if _, err := copyLinkJars(t.TempDir()); err == nil || !strings.Contains(err.Error(), "index-*.tsv yok") {
		t.Errorf("indekssiz kaynak: %v", err)
	}
}
