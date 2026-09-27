package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcos/internal/model"
)

// ════════════════════════════════════════════════════════════════════════════
// ORTAK DÜNYANIN İKİ SESSİZ KIRIK NOKTASI
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcı "mcos eşleme çalışsın" dedi. Kod okunduğunda özellik TAM görünüyor:
// bölge bölüşümü var, oyuncu verisi taşınıyor, aktarım paketi gönderiliyor.
// Ama iki yerde sessizce düşüyordu ve ikisi de yalnızca GERÇEK bir aktarım
// denendiğinde ortaya çıkardı.

// ── 1. accepts-transfers ────────────────────────────────────────────────────
//
// Minecraft'ta HEDEF sunucu, gelen aktarımı kabul etmek için bu ayarı ister ve
// VARSAYILANI FALSE'tur. Hiçbir yerde yazılmıyordu: eklentinin javadoc'u onu
// "zorunlu ön koşul" ilan ediyor ama Go tarafı yazmıyordu.
//
// Belirti: oyuncu sınırı geçer, verisi gider, paket yollanır — ve hedef
// sunucu istemciyi reddeder. Yani her şey doğru görünür, yalnızca son adım
// olmaz.
func TestAcceptsTransfersYaziliyor(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "server.properties"),
		[]byte("server-port=25565\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := &model.Server{ID: "s1", Port: 25565}
	if err := applyProperties(dir, srv); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(filepath.Join(dir, "server.properties"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "accepts-transfers=true") {
		t.Fatalf("server.properties'te accepts-transfers=true YOK — "+
			"ortak dünyada hedef sunucu aktarılan oyuncuyu REDDEDER.\n%s", b)
	}
}

// ── 2. MCOS_LINK_PORT ───────────────────────────────────────────────────────
//
// supervisor.Spec'in Env alanı vardı ve süreç onu kullanıyordu, ama hiçbir yer
// doldurmuyordu. Sonuç: her sunucu aynı sabit portu (27893) dinlemeye
// çalışıyor; aynı makinede ikinci sunucuda bind başarısız oluyor ve mod bunu
// ölümcül saymayıp yalnızca uyarı basıyor.
//
// Tek makinede birden çok sunucu tam olarak istenen şey olduğu için bu,
// özelliğin ön koşulu.
func TestLinkPortOrtamaGeciyor(t *testing.T) {
	srv := &model.Server{ID: "s1"}
	srv.Link.LinkPort = 27895

	env := linkEnv(srv)
	if !ortamVar(env, "MCOS_LINK_PORT=27895") {
		t.Errorf("MCOS_LINK_PORT ortama geçmedi — mod sabit 27893'ü dinler "+
			"ve ikinci sunucuda bind başarısız olur\n%v", ilgili(env))
	}
	if !ortamVarOnek(env, "MCOS_LINK_COORDINATOR=") {
		t.Error("MCOS_LINK_COORDINATOR yok — mod topolojiyi alamaz")
	}
	if !ortamVar(env, "MCOS_SERVER_ID=s1") {
		t.Error("MCOS_SERVER_ID yok — aynı makinedeki iki sunucu ayırt edilemez")
	}
}

// LinkPort ayarlanmamışsa varsayılana düşmeli, BOŞ kalmamalı.
func TestLinkPortVarsayilani(t *testing.T) {
	env := linkEnv(&model.Server{ID: "s2"})
	if !ortamVarOnek(env, "MCOS_LINK_PORT=") {
		t.Fatal("LinkPort yokken MCOS_LINK_PORT hiç ayarlanmadı")
	}
	for _, e := range env {
		if strings.HasPrefix(e, "MCOS_LINK_PORT=") &&
			e == "MCOS_LINK_PORT=0" {
			t.Error("MCOS_LINK_PORT=0 — mod geçersiz porta bağlanmaya çalışır")
		}
	}
}

// ── 3. Ortam TAMAMEN değiştirilmemeli ───────────────────────────────────────
//
// supervisor, Env doluysa süreç ortamını TAMAMEN onunla değiştiriyor. Yalnızca
// MCOS_* değişkenleri verseydik JVM PATH, HOME ve TZ olmadan başlardı ve bu,
// "çalışıyor ama saat yanlış / geçici dosya yazamıyor" gibi anlaşılmaz
// belirtiler üretirdi.
func TestMevcutOrtamKorunuyor(t *testing.T) {
	t.Setenv("MCOS_TEST_KANARYA", "1")
	env := linkEnv(&model.Server{ID: "s3"})
	if !ortamVar(env, "MCOS_TEST_KANARYA=1") {
		t.Error("mevcut ortam korunmuyor — JVM PATH/HOME/TZ olmadan başlar")
	}
	if !ortamVarOnek(env, "PATH=") {
		t.Error("PATH ortamda yok — Java alt süreçleri bulunamaz")
	}
}

func ortamVar(env []string, tam string) bool {
	for _, e := range env {
		if e == tam {
			return true
		}
	}
	return false
}

func ortamVarOnek(env []string, onek string) bool {
	for _, e := range env {
		if strings.HasPrefix(e, onek) {
			return true
		}
	}
	return false
}

func ilgili(env []string) []string {
	var out []string
	for _, e := range env {
		if strings.HasPrefix(e, "MCOS_") {
			out = append(out, e)
		}
	}
	return out
}
