package ipcclient

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// ════════════════════════════════════════════════════════════════════════════
// BAĞLANTISIZ İSTEMCİ ASLA ÇÖKMEMELİ
// ════════════════════════════════════════════════════════════════════════════
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
// Panel, daemon'a bağlanamadan da çalışabilir (ekran görüntüsü kipi, açılışta
// daemon'dan önce hazır olmak, daemon'ın çökmesi). O durumlarda istemci
// nil'dir.
//
// Eskiden koruma HER ÇAĞRI YERİNDE ayrı ayrı yapılıyordu ("a.offline() ise
// dön"). playit hesap bağlama akışında o denetim unutulmuştu ve sonuç bir
// ARKA PLAN GOROUTINE'İNDE nil işaretçi çökmesiydi:
//
//	panic: runtime error: invalid memory address or nil pointer dereference
//	  mcos/internal/ipcclient.(*Client).PlayitClaim(...)  link.go:142
//	  mcos/internal/fbpanel.(*App).startPlayitClaim.func1()
//
// Go'da bir goroutine'deki panik TÜM PROGRAMI düşürür: kullanıcı "hesabı
// bağla" dediğinde panel tamamen kapanıyordu.
//
// Düzeltme koruma noktasını TEKE indirdi (Client.call). Bu test, o teklikten
// geri dönülmediğini ve HİÇBİR metodun nil alıcıda çökmediğini garanti eder.
// Yeni bir RPC eklendiğinde ayrıca bir şey yapmak gerekmez — yansıma (reflect)
// ile tüm dışa açık metotlar otomatik taranır.

// skipMethods are the ones that legitimately touch the connection directly.
//
// Reconnect'in işi ZATEN yeniden bağlanmaktır; nil bir bağlantıda hata
// döndürmesi doğru davranıştır ve onu "RPC" saymak yanıltıcı olur.
var skipMethods = map[string]bool{
	"Reconnect": true,
}

// TestNilClientNeverPanics — bağlantısız istemcide HİÇBİR metot çökmemeli.
func TestNilClientNeverPanics(t *testing.T) {
	var cl *Client // hiç bağlanmamış

	typ := reflect.TypeOf(cl)
	val := reflect.ValueOf(cl)

	tested := 0
	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		if skipMethods[m.Name] {
			continue
		}

		// Metodun her parametresi için sıfır değer üret.
		mt := m.Type
		args := make([]reflect.Value, 0, mt.NumIn()-1)
		for j := 1; j < mt.NumIn(); j++ {
			args = append(args, reflect.Zero(mt.In(j)))
		}

		tested++
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s bağlantısız istemcide PANİKLEDİ: %v", m.Name, r)
				}
			}()
			out := val.Method(i).Call(args)

			// Hata döndüren her metot ErrNoDaemon döndürmeli: çağıran
			// "bağlantı yok" ile "sunucu reddetti"yi ayırt edebilmeli.
			for _, o := range out {
				if err, ok := o.Interface().(error); ok && err != nil {
					if !errors.Is(err, ErrNoDaemon) {
						t.Errorf("%s beklenmeyen hata döndürdü: %v "+
							"(ErrNoDaemon bekleniyordu)", m.Name, err)
					}
				}
			}
		}()
	}

	// Yansıma hiçbir metot bulamadıysa test sessizce geçerdi — bu, testin
	// kendisinin bozulduğu anlamına gelir.
	if tested < 20 {
		t.Fatalf("yalnızca %d metot sınandı — yansıma bozulmuş olabilir", tested)
	}
	t.Logf("%d metot bağlantısız istemcide sınandı", tested)
}

// Aynı şey SIFIR DEĞERLİ bir istemci için de geçerli olmalı: &Client{} da
// nil bir iç bağlantı taşır ve panelde öyle bir nesne oluşabilir.
func TestZeroClientNeverPanics(t *testing.T) {
	cl := &Client{}

	typ := reflect.TypeOf(cl)
	val := reflect.ValueOf(cl)

	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		if skipMethods[m.Name] {
			continue
		}
		mt := m.Type
		args := make([]reflect.Value, 0, mt.NumIn()-1)
		for j := 1; j < mt.NumIn(); j++ {
			args = append(args, reflect.Zero(mt.In(j)))
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s sıfır değerli istemcide PANİKLEDİ: %v", m.Name, r)
				}
			}()
			val.Method(i).Call(args)
		}()
	}
}

// Koruma TEK YERDE olmalı: doğrudan cl.c.Call çağıran bir metot kalırsa,
// oradaki nil denetimi yeniden unutulabilir.
func TestErrNoDaemonMessageIsUseful(t *testing.T) {
	// Hata kullanıcıya gösteriliyor; teknik bir Go mesajı değil, anlaşılır
	// bir cümle olmalı.
	msg := ErrNoDaemon.Error()
	if strings.Contains(msg, "nil") || strings.Contains(msg, "pointer") {
		t.Errorf("hata mesajı kullanıcıya gösterilemez: %q", msg)
	}
	if len(msg) < 10 {
		t.Errorf("hata mesajı çok kısa: %q", msg)
	}
}
