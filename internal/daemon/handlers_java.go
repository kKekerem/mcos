package daemon

import (
	"context"
	"encoding/json"

	"mcos/internal/ipc"
	"mcos/internal/model"
)

func (d *Daemon) handleJavaList(_ context.Context, _ json.RawMessage) (any, error) {
	rts, err := d.java.List()
	if err != nil {
		return nil, err
	}
	return ipc.JavaListResult{Runtimes: rts}, nil
}

func (d *Daemon) handleJavaResolve(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.JavaResolveParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	major, installed, err := d.java.Resolve(p.MCVersion)
	if err != nil {
		return nil, err
	}
	return ipc.JavaResolveResult{Major: major, Installed: installed}, nil
}

// handleJavaInstall installs a Java major version and replies with the runtime
// that is really on disk by the time the call returns.
//
// ── Yakalanan gerçek hata ───────────────────────────────────────────────────
// Bu işleyici indirmeyi bir goroutine'e atıp hemen
// ipc.OKResult{OK: true, Message: "Java indirmesi başlatıldı"} dönüyordu.
// Oysa yanıtı çözen İKİ istemci de (ipcclient.Client.JavaInstall ve
// cmd/mcosctl) ipc.JavaRuntimeResult bekliyor. OKResult'ta "runtime" alanı
// olmadığı için JSON çözme SESSİZCE başarılı oluyor, geriye hatasız bir
// SIFIR model.JavaRuntime kalıyordu.
//
// Kullanıcının gördüğü: ilk kurulum sihirbazında "Gerekli Java'yı kur"a
// basıldıktan milisaniyeler sonra "Java 0 kuruldu" yazısı, ve özet ekranında
// kurulu görünen bir Java — tek bayt inmeden. İndirme sonradan başarısız
// olursa (ayna kapalı, disk dolu, Wi-Fi düştü) bunu düzelten hiçbir şey
// yoktu; hata ancak kullanıcı ilk sunucusunu kurup "java bulunamadı" ile
// karşılaştığında ortaya çıkıyordu. Ana panelde aynı hata, sürüm dizesi de
// boş olduğu için "Java 0 kuruldu ()" diye basılıyordu.
//
// Artık çağrı indirme + açma bitene kadar BEKLER: dönen hata gerçek hatadır,
// dönen sürüm gerçekten kurulmuş sürümdür. Bilinçli olarak yavaş bir RPC'dir;
// her iki çağıran da bunu arka plan goroutine'inden yapıyor ve süre boyunca
// "indiriliyor" göstergesini açık tutuyor.
func (d *Daemon) handleJavaInstall(ctx context.Context, raw json.RawMessage) (any, error) {
	var p ipc.JavaInstallParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if p.Major <= 0 {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: "major must be > 0"}
	}

	type installOutcome struct {
		rt  *model.JavaRuntime
		err error
	}
	// Kurulum ayrı bir goroutine'de koşar, işleyici onu BEKLER.
	//
	// Neden doğrudan çağırmıyoruz: daemon kapanırken (ctx iptal) işleyicinin
	// dakikalarca asılı kalmaması gerekiyor. Kanal TAMPONLU; ctx yüzünden
	// erken dönsek bile goroutine sonucunu yazıp çıkar, sızmaz. İndirme kendi
	// hâlinde tamamlanır ve java.progress üzerinden izlenmeye devam eder.
	done := make(chan installOutcome, 1)
	go func() {
		rt, err := d.java.Install(p.Major)
		done <- installOutcome{rt: rt, err: err}
	}()

	select {
	case out := <-done:
		if out.err != nil {
			return nil, out.err
		}
		if out.rt == nil {
			// Olmaması gereken durum; yine de SIFIR bir runtime döndürüp
			// paneli "kuruldu" demeye ikna etmemeli.
			return nil, &ipc.Error{Code: ipc.CodeInternalError, Message: "java kurulumu sonuç döndürmedi"}
		}
		return ipc.JavaRuntimeResult{Runtime: *out.rt}, nil
	case <-ctx.Done():
		return nil, &ipc.Error{Code: ipc.CodeInternalError, Message: "java kurulumu yarıda kesildi: " + ctx.Err().Error()}
	}
}

func (d *Daemon) handleJavaRemove(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.JavaInstallParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := d.java.Remove(p.Major); err != nil {
		return nil, err
	}
	return ipc.OKResult{OK: true}, nil
}

func (d *Daemon) handleJavaDetect(_ context.Context, _ json.RawMessage) (any, error) {
	rts, err := d.java.Detect()
	if err != nil {
		return nil, err
	}
	return ipc.JavaListResult{Runtimes: rts}, nil
}

func (d *Daemon) handleJavaProgress(_ context.Context, _ json.RawMessage) (any, error) {
	return ipc.JavaProgressResult{Progresses: d.java.ProgressMap()}, nil
}
