// Package deskgui gives the desktop tools (mcos-node, mcos-flash) a small,
// clean window instead of a console screen.
//
// ════════════════════════════════════════════════════════════════════════════
// NEDEN BU YOL
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcının isteği: "Windows exe'sini derle, basit bir GUI ekle,
// eşleştirme GUI'si yap." Konsol ekranı çalışıyordu ama Windows kullanıcısı
// çift tıklayınca siyah bir pencere görüyor, anahtarı oraya yapıştırmakta
// zorlanıyor ve "bu program bozuk mu" diye soruyordu.
//
// Dağıtılan ikililer CGO'SUZ derleniyor (tek dosya, çapraz derleme, imzasız
// DLL yok). Bu, GTK/Qt/Win32 sarmalayıcılarının çoğunu dışarıda bırakır.
// Geriye kalan en sağlam yol:
//
//   - arayüz gömülü bir HTML/JS sayfasıdır,
//   - YALNIZCA 127.0.0.1'de, rastgele bir portta ve rastgele bir belirteçle
//     sunulur (bkz. Server),
//   - Windows'ta WebView2 penceresinde açılır (Windows 10/11'de hazır gelen
//     Edge motoru; jchv/go-webview2 onu CGO'suz yükler),
//   - WebView2 yoksa varsayılan tarayıcıda, Linux'ta ise adres yazdırılır.
//
// ════════════════════════════════════════════════════════════════════════════
// GÜVENLİK
// ════════════════════════════════════════════════════════════════════════════
//
// Arayüz, ayar değiştiren (eşleştirme anahtarı) ve mcos-flash'ta DİSK SİLEN
// uçlar sunar. Bu yüzden:
//
//  1. Dinleyici 127.0.0.1'e bağlanır: ağdaki başka bir makine ulaşamaz ve
//     Windows güvenlik duvarı uyarısı çıkmaz (geri döngü uyarı üretmez).
//  2. Her istek rastgele 256 bitlik bir belirteç ister. Aynı makinedeki
//     başka bir kullanıcı ya da süreç portu bulsa bile belirteç olmadan
//     hiçbir uca erişemez. Sayfa belirteci adres satırındaki ?t= ile alır;
//     API çağrıları onu ÖZEL BİR BAŞLIKTA taşır.
//  3. Özel başlık şartı, internetteki bir sayfanın tarayıcı üzerinden
//     127.0.0.1'e sahte istek göndermesini (CSRF) engeller: başka kökenden
//     özel başlıklı istek CORS ön denetimi ister ve biz ona izin vermeyiz.
//  4. Host başlığı denetlenir: DNS yeniden bağlama (rebinding) saldırısında
//     tarayıcı Host olarak saldırganın alan adını gönderir.
package deskgui

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// TokenHeader is the header every API request must carry.
const TokenHeader = "X-MCOS-Belirtec"

// maxBody caps a request body: the API only takes short JSON objects.
const maxBody = 64 << 10

// Page is the embedded interface of one program.
type Page struct {
	// Title is the document (and window) title.
	Title string
	// Body is the inner HTML of <body>.
	Body string
	// Script is the program's own JavaScript (runs after the shared helpers).
	Script string
	// Style is extra CSS appended after the shared theme.
	Style string
}

// Server serves one Page and its API on a random loopback port.
type Server struct {
	token string
	host  string // "127.0.0.1:PORT": Host başlığı bununla karşılaştırılır
	port  int
	ln    net.Listener
	mux   *http.ServeMux
	page  Page
	srv   *http.Server
	// last, doğrulanmış son isteğin zamanıdır (UnixNano). Tarayıcı kipinde
	// sekme kapanınca programın da kapanabilmesi için (bkz. Run).
	last atomic.Int64
}

// Error is an API failure with an HTTP status and a Turkish message.
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

// Fail builds an *Error (400 unless code says otherwise).
func Fail(code int, format string, args ...any) error {
	if code == 0 {
		code = http.StatusBadRequest
	}
	return &Error{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// New opens the loopback listener and prepares the page.
func New(p Page) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("arayüz için yerel port açılamadı: %w", err)
	}
	tok, err := newToken()
	if err != nil {
		ln.Close()
		return nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	s := &Server{
		token: tok,
		port:  port,
		host:  "127.0.0.1:" + strconv.Itoa(port),
		ln:    ln,
		mux:   http.NewServeMux(),
		page:  p,
	}
	s.mux.HandleFunc("/", s.servePage)
	s.srv = &http.Server{
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s, nil
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("belirteç üretilemedi: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Token returns the secret (tests and the window opener need it).
func (s *Server) Token() string { return s.token }

// URL is the address the window opens: page plus token.
func (s *Server) URL() string { return "http://" + s.host + "/?t=" + s.token }

// Start serves in the background until Close.
func (s *Server) Start() {
	go func() { _ = s.srv.Serve(s.ln) }()
}

// Close stops the server.
func (s *Server) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.srv.Shutdown(ctx)
}

// LastSeen is when the page last made an authenticated request.
func (s *Server) LastSeen() time.Time {
	n := s.last.Load()
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n)
}

// Get registers a read-only API endpoint.
func (s *Server) Get(path string, h func(r *http.Request) (any, error)) {
	s.mux.HandleFunc(path, s.api(http.MethodGet, h))
}

// Post registers an endpoint that changes something.
func (s *Server) Post(path string, h func(r *http.Request) (any, error)) {
	s.mux.HandleFunc(path, s.api(http.MethodPost, h))
}

// ServeHTTP applies the host check to every request, then routes.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.hostOK(r.Host) {
		http.Error(w, "yasak", http.StatusForbidden)
		return
	}
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
	s.mux.ServeHTTP(w, r)
}

// hostOK accepts only our own loopback address.
func (s *Server) hostOK(host string) bool {
	return host == s.host || host == "localhost:"+strconv.Itoa(s.port)
}

func (s *Server) tokenOK(got string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
}

// servePage returns the interface — only with the right ?t=.
func (s *Server) servePage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "yöntem desteklenmiyor", http.StatusMethodNotAllowed)
		return
	}
	if !s.tokenOK(r.URL.Query().Get("t")) {
		http.Error(w, "belirteç eksik ya da yanlış", http.StatusForbidden)
		return
	}
	s.last.Store(time.Now().UnixNano())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Satır içi betik ve stil bizim; dışarıdan hiçbir şey yüklenmez.
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; "+
			"connect-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'none'; "+
			"frame-ancestors 'none'")
	_, _ = w.Write(s.render())
}

// render builds the full HTML document.
func (s *Server) render() []byte {
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"tr\"><head><meta charset=\"utf-8\">")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">")
	b.WriteString("<meta name=\"color-scheme\" content=\"dark\">")
	b.WriteString("<meta name=\"mcos-belirtec\" content=\"" + html.EscapeString(s.token) + "\">")
	b.WriteString("<title>" + html.EscapeString(s.page.Title) + "</title>")
	b.WriteString("<style>" + themeCSS + s.page.Style + "</style></head><body>")
	b.WriteString(s.page.Body)
	b.WriteString("<script>" + baseJS + "</script>")
	b.WriteString("<script>" + s.page.Script + "</script>")
	b.WriteString("</body></html>")
	return []byte(b.String())
}

// api wraps a handler with method, token and JSON handling.
func (s *Server) api(method string, h func(r *http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.tokenOK(r.Header.Get(TokenHeader)) {
			writeJSON(w, http.StatusForbidden, map[string]any{"hata": "belirteç eksik ya da yanlış"})
			return
		}
		if r.Method != method {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"hata": "yöntem desteklenmiyor"})
			return
		}
		s.last.Store(time.Now().UnixNano())
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		v, err := h(r)
		if err != nil {
			code := http.StatusInternalServerError
			var e *Error
			if errors.As(err, &e) {
				code = e.Code
			}
			writeJSON(w, code, map[string]any{"hata": err.Error()})
			return
		}
		if v == nil {
			v = map[string]any{"tamam": true}
		}
		writeJSON(w, http.StatusOK, v)
	}
}

// ReadJSON decodes a request body; a bad body is a 400.
func ReadJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return Fail(http.StatusBadRequest, "istek okunamadı: %v", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
