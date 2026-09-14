// Package remote exposes the daemon's JSON-RPC surface over HTTPS so the
// Android app (and anything else) can control MCOS from another machine.
//
// ════════════════════════════════════════════════════════════════════════════
// TASARIM
// ════════════════════════════════════════════════════════════════════════════
//
// ── Neden ayrı bir dinleyici, ayrı bir protokol DEĞİL ───────────────────────
// Panel ve mcosctl, mcosd ile satır satır JSON-RPC konuşur. Uzaktan kontrol
// için İKİNCİ bir yöntem tablosu yazmak, zamanla iki yolun ayrışmasına yol
// açardı: panelde çalışan bir şey telefonda çalışmaz, ya da tersi.
//
// Burası yalnızca bir KÖPRÜ: HTTP gövdesini alır, aynı ipc.Server'a verir,
// yanıtı geri yazar. Yeni bir RPC eklendiğinde telefonda da kendiliğinden
// çalışır.
//
// ── Neden HTTP ──────────────────────────────────────────────────────────────
// Ham TCP üzerinde satır protokolü de yazılabilirdi, ama telefon tarafında
// her şey (yeniden bağlanma, zaman aşımı, HTTP/2, sertifika sabitleme) hazır
// gelir. Flutter'da 15 satırlık bir istemci yeter.
//
// ── Güvenlik ────────────────────────────────────────────────────────────────
//  1. TLS ZORUNLU (bkz. cert.go): jeton ağda açık gitmez.
//  2. Her istek bir jeton taşır; karşılaştırma sabit sürelidir.
//  3. Başarısız kimlik denemeleri IP başına yavaşlatılır.
//  4. Jeton yapılandırılmamışsa dinleyici HİÇ AÇILMAZ — "kapalı" ile
//     "herkese açık" arasında kaza eseri geçiş olmasın.
package remote

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"mcos/internal/ipc"
	"mcos/internal/log"
)

// Dispatcher runs one JSON-RPC request line and returns the response.
//
// *ipc.Server bunu karşılar. Arayüz olarak durması, testlerin gerçek daemon'a
// ihtiyaç duymamasını sağlıyor.
type Dispatcher interface {
	Dispatch(ctx context.Context, line []byte) ipc.Response
}

// Options configures the remote bridge.
type Options struct {
	// Addr, dinlenecek adres ("0.0.0.0:2223").
	Addr string
	// Token, her istekte beklenen gizli değer. BOŞSA sunucu açılmaz.
	Token string
	// CertDir, TLS sertifikasının saklandığı KALICI klasör.
	CertDir string
	// Name, /health yanıtında görünen cihaz adı (telefonda listelenir).
	Name string
	// Version, MCOS sürümü.
	Version string
	Log     *log.Logger
}

// Server is the HTTPS bridge.
type Server struct {
	opt   Options
	disp  Dispatcher
	cert  *certificate
	http  *http.Server
	ln    net.Listener
	limit *authLimiter
}

// maxBodyBytes bounds one request.
//
// 8 MiB: en büyük gerçek istek dosya yazmadır (server.files.write). Sınırsız
// bırakmak, tek bir isteğin belleği tüketmesine izin vermek olurdu.
const maxBodyBytes = 8 << 20

// New creates the bridge. It does not listen yet.
func New(d Dispatcher, o Options) (*Server, error) {
	if d == nil {
		return nil, errors.New("remote: dispatcher yok")
	}
	if strings.TrimSpace(o.Token) == "" {
		// Bu bir HATA, sessiz bir varsayılan değil: jetonsuz açılan bir
		// dinleyici, ağdaki herkese sunucunun tam denetimini verirdi.
		return nil, errors.New("remote: jeton yok — uzaktan erişim açılamaz")
	}
	if o.Addr == "" {
		o.Addr = fmt.Sprintf(":%d", DefaultPort)
	}
	if o.CertDir == "" {
		return nil, errors.New("remote: sertifika klasörü belirtilmedi")
	}

	cert, err := loadOrCreateCert(o.CertDir)
	if err != nil {
		return nil, err
	}

	s := &Server{opt: o, disp: d, cert: cert, limit: newAuthLimiter()}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/rpc", s.handleRPC)

	s.http = &http.Server{
		Handler: mux,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert.tls},
			// TLS 1.2 tabanı: daha eskisi kırık. Android 5+ ve her masaüstü
			// istemcisi 1.2 konuşur.
			MinVersion: tls.VersionTLS12,
		},
		// Zaman aşımları: yavaş ya da kötü niyetli bir istemci bağlantıyı
		// süresiz tutamamalı.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second, // uzun RPC'ler (indirme) için bol
		IdleTimeout:       90 * time.Second,
	}
	return s, nil
}

// DefaultPort is where the remote bridge listens.
//
// 2223: eşleştirme portunun (2222) hemen yanı — kullanıcı iki sayıyı birlikte
// hatırlar. Ayrıcalıklı aralığın dışında olduğu için kök gerektirmez.
const DefaultPort = 2223

// Fingerprint returns the certificate fingerprint users verify on the phone.
func (s *Server) Fingerprint() string { return s.cert.fingerprint }

// Addr returns the address actually bound, valid after Serve starts.
func (s *Server) Addr() string {
	if s.ln == nil {
		return s.opt.Addr
	}
	return s.ln.Addr().String()
}

// Serve listens until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.opt.Addr)
	if err != nil {
		return fmt.Errorf("uzaktan erişim portu açılamadı (%s): %w", s.opt.Addr, err)
	}
	s.ln = ln

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.http.Shutdown(shutCtx)
	}()

	if s.opt.Log != nil {
		s.opt.Log.Infof("remote: uzaktan erişim açık (%s), parmak izi %s",
			ln.Addr(), s.cert.fingerprint)
	}

	err = s.http.ServeTLS(ln, "", "")
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Close stops the bridge.
func (s *Server) Close() error { return s.http.Close() }

// ── Uç noktalar ─────────────────────────────────────────────────────────────

// healthReply is what an unauthenticated caller may learn.
//
// BİLEREK YOKSUL: sunucu listesi, kullanıcı adı, sürüm ayrıntısı gibi hiçbir
// şey yok. Amaç yalnızca telefonun "doğru adrese mi bağlandım" sorusunu
// yanıtlamak.
type healthReply struct {
	Service     string `json:"service"`
	Version     string `json:"version"`
	Name        string `json:"name,omitempty"`
	Fingerprint string `json:"fingerprint"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "yalnızca GET", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, healthReply{
		Service:     "mcos",
		Version:     s.opt.Version,
		Name:        s.opt.Name,
		Fingerprint: s.cert.fingerprint,
	})
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "yalnızca POST", http.StatusMethodNotAllowed)
		return
	}

	ip := clientIP(r)
	if wait := s.limit.penalty(ip); wait > 0 {
		// Kaba kuvvet denemesini yavaşlat. Yanıtı geciktirmek, saldırganın
		// saniyede deneyebileceği jeton sayısını düşürür.
		time.Sleep(wait)
	}

	if !s.authorized(r) {
		s.limit.fail(ip)
		if s.opt.Log != nil {
			s.opt.Log.Warnf("remote: %s adresinden yetkisiz istek", ip)
		}
		// 401 ve BAŞKA HİÇBİR ŞEY: jetonun neden yanlış olduğunu
		// söylemek saldırgana bilgi verir.
		http.Error(w, "yetkisiz", http.StatusUnauthorized)
		return
	}
	s.limit.ok(ip)

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		http.Error(w, "istek okunamadı", http.StatusBadRequest)
		return
	}
	if len(body) > maxBodyBytes {
		http.Error(w, "istek çok büyük", http.StatusRequestEntityTooLarge)
		return
	}
	body = trimLine(body)
	if len(body) == 0 {
		http.Error(w, "boş istek", http.StatusBadRequest)
		return
	}

	resp := s.disp.Dispatch(r.Context(), body)
	// HTTP durumu HER ZAMAN 200: hata JSON-RPC zarfının içindedir. Bu,
	// istemcinin tek bir yerde hata araması demek.
	writeJSON(w, http.StatusOK, resp)
}

// authorized reports whether the request carries the right token.
func (s *Server) authorized(r *http.Request) bool {
	got := bearerToken(r)
	if got == "" {
		return false
	}
	// Sabit süreli karşılaştırma: erken çıkan bir karşılaştırma, doğru
	// önekin uzunluğunu zamanlamadan sızdırır.
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.opt.Token)) == 1
}

// bearerToken reads the token from either supported header.
func bearerToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
			return strings.TrimSpace(h[7:])
		}
	}
	// X-MCOS-Token: bazı istemcilerde Authorization başlığını ayarlamak
	// zahmetli (ör. bir kabuk betiğinden curl). İkisi de kabul edilir.
	return strings.TrimSpace(r.Header.Get("X-MCOS-Token"))
}

func clientIP(r *http.Request) string {
	// X-Forwarded-For BİLEREK okunmuyor: burada güvenilir bir vekil yok ve
	// saldırgan o başlığı istediği gibi yazabilir; yavaşlatmayı atlatmak
	// için her istekte farklı bir değer göndermesi yeterdi.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// Yanıtlar önbelleğe alınmamalı: durum sürekli değişiyor.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func trimLine(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\n' || b[0] == '\r' || b[0] == '\t') {
		b = b[1:]
	}
	return b
}

// ── Kaba kuvvet yavaşlatma ──────────────────────────────────────────────────

// authLimiter slows down repeated authentication failures per IP.
//
// Jeton 128 bit rastgele olduğu için kaba kuvvet zaten umutsuz; bu katman,
// jetonun kısa ya da tahmin edilebilir olduğu durumlarda (kullanıcı kendi
// jetonunu elle yazarsa) bir emniyet payı sağlar. Ayrıca günlükte saldırıyı
// görünür kılar.
type authLimiter struct {
	mu    sync.Mutex
	fails map[string]int
	seen  map[string]time.Time
}

func newAuthLimiter() *authLimiter {
	return &authLimiter{fails: map[string]int{}, seen: map[string]time.Time{}}
}

// maxPenalty caps the slowdown so a forgotten attacker cannot lock a
// legitimate user out for minutes.
const maxPenalty = 2 * time.Second

func (l *authLimiter) penalty(ip string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.expire()
	n := l.fails[ip]
	if n <= 2 {
		return 0 // ilk birkaç hata: kullanıcı jetonu yanlış yapıştırmış olabilir
	}
	d := time.Duration(n-2) * 200 * time.Millisecond
	if d > maxPenalty {
		d = maxPenalty
	}
	return d
}

func (l *authLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.expire()
	l.fails[ip]++
	l.seen[ip] = time.Now()
}

func (l *authLimiter) ok(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, ip)
	delete(l.seen, ip)
}

// expire drops entries older than the window.
//
// NEDEN GEREKLİ: harita yalnızca büyürse, uzun süre çalışan bir daemon'da
// her deneme yapan IP sonsuza dek bellekte kalırdı.
func (l *authLimiter) expire() {
	const window = 10 * time.Minute
	now := time.Now()
	for ip, t := range l.seen {
		if now.Sub(t) > window {
			delete(l.seen, ip)
			delete(l.fails, ip)
		}
	}
}
