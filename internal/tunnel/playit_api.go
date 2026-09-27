package tunnel

// ════════════════════════════════════════════════════════════════════════════
// playit.gg API İSTEMCİSİ — tüneli MCOS'un kendisi açar
// ════════════════════════════════════════════════════════════════════════════
//
// Kullanıcı: "playit ajanı gerçek değil; siteye giriyorum, ajanın çevrimiçi
// olmasını bekliyor". Anahtar kaydı düzeltildikten sonra da ekranda yalnızca
// "playit.gg sitesinden tünel oluşturun" yazıyordu. Neden: "claim exchange"
// ajanı SELF-MANAGED türünde kaydeder (CLI'nin varsayılanı; "claim url
// --help": "--type <TYPE> [default: self-managed]"). Self-managed bir ajana
// playit bulutu kendiliğinden tünel AÇMAZ — tüneli ajanın sahibi açmalıdır.
// Sahibi de biziz: aynı gizli anahtarla (Agent-Key) API'ye tünel oluşturma
// isteği gönderiyoruz; playitd 1.0.10 tünelleri /v1/agents/rundata'dan okur
// ve yeni tüneli yeniden başlatılmadan yönlendirir.
//
// ── Uç noktalar (resmî playit-agent kaynağından) ────────────────────────────
//
//	POST /v1/agents/rundata   {}             → ajan kimliği + tüneller (yeni)
//	POST /agents/rundata      {}             → aynısı, eski biçim (yedek)
//	POST /v1/tunnels/create   {ports,origin} → {"id": ...}         (yeni)
//	POST /tunnels/create      {tunnel_type…} → {"id": ...}         (eski;
//	                                            CLI 0.15 "tunnels prepare")
//
// Her yanıt bir zarf içindedir:
//
//	{"status":"success","data":…}
//	{"status":"fail","data":"RequiresVerifiedAccount"}      (birimli enum → DİZGE)
//	{"status":"error","data":{"type":"auth","message":"InvalidAgentKey"}}
//
// api.playit.gg'ye anahtarsız ve UYDURMA anahtarla gönderilen isteklerin
// GERÇEK yanıtları playit_api_test.go'da fikstür olarak duruyor (2026-09-27'de
// ölçüldü). Dikkat: "path-not-found" iletisi DİZGE DEĞİL NESNEDİR
// ({"path":"/v1/…"}); iletiyi dizge sanıp çözmek bütün zarfı düşürürdü.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// PlayitAPIBase is the playit.gg API root. Değişken: sınamalar httptest
// sunucusuna yönlendirir.
var PlayitAPIBase = "https://api.playit.gg"

// playitAPITimeout bounds one API request.
//
// 15 sn: panel RPC'leri 10 sn'de zaman aşımına uğrar ama API çağrıları hiçbir
// zaman RPC içinde yapılmaz (arka plan eşitleyicisi); yavaş bir mobil hatta
// TLS el sıkışması birkaç saniye sürebiliyor.
const playitAPITimeout = 15 * time.Second

// defaultRetryAfter is used when a 429 carries no Retry-After header.
const defaultRetryAfter = 30 * time.Second

// TunnelTypeMinecraftJava is playit's tunnel type for Java Edition servers.
const TunnelTypeMinecraftJava = "minecraft-java"

// API error kinds. "auth", "validation", "path-not-found" ve "internal"
// playit'in kendi "error.type" değerleridir; diğerleri istemci tarafıdır.
const (
	KindFail         = "fail"
	KindAuth         = "auth"
	KindValidation   = "validation"
	KindPathNotFound = "path-not-found"
	KindInternal     = "internal"
	KindRateLimit    = "rate-limit"
	KindNetwork      = "network"
	KindHTTP         = "http"
	KindShape        = "shape" // yanıt beklenen biçimde değil
)

// APIError is a decoded playit API failure.
//
// Error() KULLANICIYA gösterilecek Türkçe cümleyi döndürür (bkz.
// playit_errors.go); ham enum Code'da, günlük için Detail'de durur.
type APIError struct {
	Kind       string
	Code       string // "RequiresVerifiedAccount", "InvalidAgentKey", …
	Path       string // istenen API yolu
	HTTPStatus int
	RetryAfter time.Duration // yalnızca KindRateLimit
	Detail     string
}

func (e *APIError) Error() string { return PlayitMessage(e) }

// RateLimited reports whether err is a 429, and how long to wait.
func RateLimited(err error) (time.Duration, bool) {
	var ae *APIError
	if errors.As(err, &ae) && ae.Kind == KindRateLimit {
		return ae.RetryAfter, true
	}
	return 0, false
}

// KeyInvalid reports whether err means the stored secret is useless and the
// account must be linked again.
func KeyInvalid(err error) bool {
	var ae *APIError
	if !errors.As(err, &ae) || ae.Kind != KindAuth {
		return false
	}
	switch ae.Code {
	case "AuthRequired", "InvalidAgentKey", "InvalidApiKey", "SessionExpired",
		"NoLongerValid", "AccountDoesNotExist":
		return true
	}
	return false
}

// PlayitAPI talks to api.playit.gg with the agent secret.
type PlayitAPI struct {
	Base   string
	Secret string
	HTTP   *http.Client
}

// NewPlayitAPI builds a client from the stored secret (SecretPath).
func NewPlayitAPI() (*PlayitAPI, error) {
	secret, err := ReadSecret()
	if err != nil {
		return nil, err
	}
	return &PlayitAPI{
		Base:   PlayitAPIBase,
		Secret: secret,
		HTTP:   &http.Client{Timeout: playitAPITimeout},
	}, nil
}

// ReadSecret returns the agent secret stored at SecretPath.
//
// Dosya `secret_key = "…"` biçimindedir (SaveSecret yazar; playitd de aynısını
// okur). Değeri onaltılık olup olmadığına bakmadan alıyoruz: biçim denetimi
// playit'in işi, bizimki anahtarı olduğu gibi iletmek.
func ReadSecret() (string, error) {
	b, err := os.ReadFile(SecretPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrNotClaimed
		}
		return "", err
	}
	for _, satir := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(satir), "=")
		if ok && strings.TrimSpace(k) == "secret_key" {
			if s := strings.Trim(strings.TrimSpace(v), "\"'"); s != "" {
				return s, nil
			}
		}
	}
	if s := parseSecret(string(b)); s != "" {
		return s, nil
	}
	return "", ErrNotClaimed
}

// call POSTs req to path and decodes the success payload into out.
func (c *PlayitAPI) call(path string, req, out any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	base := c.Base
	if base == "" {
		base = PlayitAPIBase
	}
	hreq, err := http.NewRequest(http.MethodPost, strings.TrimRight(base, "/")+path,
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "application/json")
	// Anahtar KIRPILARAK gönderilir: dosyadaki sondaki satır sonu başlığa
	// girerse playit "InvalidHeader" döner (gerçek yanıt fikstürlerde).
	hreq.Header.Set("Authorization", "Agent-Key "+strings.TrimSpace(c.Secret))
	hreq.Header.Set("User-Agent", "mcos-playit/1")

	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: playitAPITimeout}
	}
	resp, err := hc.Do(hreq)
	if err != nil {
		return &APIError{Kind: KindNetwork, Path: path, Detail: err.Error()}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode == http.StatusTooManyRequests {
		return &APIError{Kind: KindRateLimit, Code: "TooManyRequests", Path: path,
			HTTPStatus: resp.StatusCode,
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}

	var env struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || env.Status == "" {
		kind := KindShape
		if resp.StatusCode >= 400 {
			kind = KindHTTP
		}
		return &APIError{Kind: kind, Path: path, HTTPStatus: resp.StatusCode,
			Detail: snippet(raw)}
	}

	switch env.Status {
	case "success":
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(env.Data, out); err != nil {
			return &APIError{Kind: KindShape, Path: path, HTTPStatus: resp.StatusCode,
				Detail: err.Error()}
		}
		return nil
	case "fail":
		return &APIError{Kind: KindFail, Code: enumName(env.Data), Path: path,
			HTTPStatus: resp.StatusCode, Detail: snippet(env.Data)}
	case "error":
		var e struct {
			Type    string          `json:"type"`
			Message json.RawMessage `json:"message"`
		}
		_ = json.Unmarshal(env.Data, &e)
		ae := &APIError{Kind: e.Type, Path: path, HTTPStatus: resp.StatusCode,
			Detail: snippet(e.Message)}
		switch e.Type {
		case KindAuth:
			ae.Code = enumName(e.Message)
		case KindPathNotFound:
			// Gerçek yanıt: {"type":"path-not-found","message":{"path":"/v1/…"}}
			var p struct {
				Path string `json:"path"`
			}
			if json.Unmarshal(e.Message, &p) == nil && p.Path != "" {
				ae.Detail = p.Path
			}
		case "":
			ae.Kind = KindShape
		}
		return ae
	}
	return &APIError{Kind: KindShape, Path: path, HTTPStatus: resp.StatusCode,
		Detail: "bilinmeyen durum: " + env.Status}
}

// enumName extracts a serde enum variant name.
//
// Birimli değişken DİZGE olarak gelir ("RequiresVerifiedAccount"); veri
// taşıyan değişken ya {"Ad": …} ya da {"type": "Ad", …} biçimindedir.
func enumName(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	if t, ok := m["type"]; ok {
		if json.Unmarshal(t, &s) == nil {
			return s
		}
	}
	if len(m) == 1 {
		for k := range m {
			return k
		}
	}
	return ""
}

// rawText renders a loosely-typed JSON value (null | string | object) as text.
func rawText(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	if n := enumName(raw); n != "" {
		return n
	}
	return snippet(raw)
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "…"
	}
	return s
}

// parseRetryAfter reads a Retry-After header (seconds or HTTP date).
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil && t.After(now) {
		return t.Sub(now)
	}
	return defaultRetryAfter
}

// ── Çalışma verisi (rundata) ────────────────────────────────────────────────

// PlayitTunnel is one tunnel (or pending tunnel) of the agent.
type PlayitTunnel struct {
	ID         string
	Name       string
	TunnelType string // "minecraft-java", boş = genel tcp/udp
	PortType   string // "tcp" | "udp" | "both"
	// Address is what players type (display_address). Bekleyen tünelde boş.
	Address   string
	LocalIP   string
	LocalPort int
	// Disabled is playit's disabled_reason (boş = etkin).
	Disabled string
	// Pending: playit tüneli henüz ayırmadı; StatusMsg nedenini söyler.
	Pending   bool
	StatusMsg string
}

// PlayitNotice is an account notice shown on the playit dashboard.
type PlayitNotice struct {
	Priority    string
	Message     string
	ResolveLink string
}

// PlayitRunData is the agent's cloud-side state.
type PlayitRunData struct {
	AgentID       string
	AgentType     string // eski uçta dolu: "default" | "assignable" | "self-managed"
	AccountStatus string // "guest" | "email-not-verified" | "verified" | …
	Tunnels       []PlayitTunnel
	Pending       []PlayitTunnel
	Notices       []PlayitNotice
	// Legacy: veri eski /agents/rundata ucundan geldi.
	Legacy bool
}

type rundataV1 struct {
	AgentID string `json:"agent_id"`
	Tunnels []struct {
		ID             string          `json:"id"`
		Name           string          `json:"name"`
		DisplayAddress string          `json:"display_address"`
		PortType       string          `json:"port_type"`
		PortCount      int             `json:"port_count"`
		TunnelType     string          `json:"tunnel_type"`
		DisabledReason json.RawMessage `json:"disabled_reason"`
		AgentConfig    struct {
			Fields []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"fields"`
		} `json:"agent_config"`
	} `json:"tunnels"`
	Pending []struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		TunnelType string `json:"tunnel_type"`
		PortType   string `json:"port_type"`
		PortCount  int    `json:"port_count"`
		StatusMsg  string `json:"status_msg"`
	} `json:"pending"`
	Notices []struct {
		Priority    string `json:"priority"`
		Message     string `json:"message"`
		ResolveLink string `json:"resolve_link"`
	} `json:"notices"`
	Permissions struct {
		IsSelfManaged bool   `json:"is_self_managed"`
		HasPremium    bool   `json:"has_premium"`
		AccountStatus string `json:"account_status"`
	} `json:"permissions"`
}

func (v *rundataV1) convert() *PlayitRunData {
	rd := &PlayitRunData{AgentID: v.AgentID, AccountStatus: v.Permissions.AccountStatus}
	if v.Permissions.IsSelfManaged {
		rd.AgentType = "self-managed"
	}
	for _, t := range v.Tunnels {
		pt := PlayitTunnel{ID: t.ID, Name: t.Name, TunnelType: t.TunnelType,
			PortType: t.PortType, Address: t.DisplayAddress,
			Disabled: rawText(t.DisabledReason)}
		for _, f := range t.AgentConfig.Fields {
			switch f.Name {
			case "local_ip":
				pt.LocalIP = strings.TrimSpace(f.Value)
			case "local_port":
				pt.LocalPort, _ = strconv.Atoi(strings.TrimSpace(f.Value))
			}
		}
		// playitd 1.0.10 ile AYNI varsayılanlar: yerel IP yoksa 127.0.0.1,
		// yerel port yoksa genel adresteki port.
		if pt.LocalIP == "" {
			pt.LocalIP = "127.0.0.1"
		}
		if pt.LocalPort == 0 {
			pt.LocalPort = portOf(t.DisplayAddress)
		}
		rd.Tunnels = append(rd.Tunnels, pt)
	}
	for _, p := range v.Pending {
		rd.Pending = append(rd.Pending, PlayitTunnel{ID: p.ID, Name: p.Name,
			TunnelType: p.TunnelType, PortType: p.PortType, Pending: true,
			StatusMsg: p.StatusMsg})
	}
	for _, n := range v.Notices {
		rd.Notices = append(rd.Notices, PlayitNotice{Priority: n.Priority,
			Message: n.Message, ResolveLink: n.ResolveLink})
	}
	return rd
}

type rundataOld struct {
	AgentID       string `json:"agent_id"`
	AgentType     string `json:"agent_type"`
	AccountStatus string `json:"account_status"`
	Tunnels       []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Port struct {
			From int `json:"from"`
			To   int `json:"to"`
		} `json:"port"`
		Proto          string          `json:"proto"`
		LocalIP        string          `json:"local_ip"`
		LocalPort      int             `json:"local_port"`
		TunnelType     string          `json:"tunnel_type"`
		AssignedDomain string          `json:"assigned_domain"`
		CustomDomain   string          `json:"custom_domain"`
		Disabled       json.RawMessage `json:"disabled"`
	} `json:"tunnels"`
	Pending []struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Proto      string `json:"proto"`
		PortCount  int    `json:"port_count"`
		TunnelType string `json:"tunnel_type"`
		IsDisabled bool   `json:"is_disabled"`
	} `json:"pending"`
}

func (v *rundataOld) convert() *PlayitRunData {
	rd := &PlayitRunData{AgentID: v.AgentID, AgentType: v.AgentType,
		AccountStatus: v.AccountStatus, Legacy: true}
	for _, t := range v.Tunnels {
		domain := t.CustomDomain
		if domain == "" {
			domain = t.AssignedDomain
		}
		addr := domain
		// Java tünelinin alan adı SRV kaydı taşır: oyuncu yalnızca alan adını
		// yazar. Diğer türlerde port olmadan adres işe yaramaz.
		if domain != "" && t.TunnelType != TunnelTypeMinecraftJava && t.Port.From > 0 {
			addr = fmt.Sprintf("%s:%d", domain, t.Port.From)
		}
		pt := PlayitTunnel{ID: t.ID, Name: t.Name, TunnelType: t.TunnelType,
			PortType: t.Proto, Address: addr, LocalIP: t.LocalIP,
			LocalPort: t.LocalPort, Disabled: rawText(t.Disabled)}
		if pt.LocalIP == "" {
			pt.LocalIP = "127.0.0.1"
		}
		if pt.LocalPort == 0 {
			pt.LocalPort = t.Port.From
		}
		rd.Tunnels = append(rd.Tunnels, pt)
	}
	for _, p := range v.Pending {
		pt := PlayitTunnel{ID: p.ID, Name: p.Name, TunnelType: p.TunnelType,
			PortType: p.Proto, Pending: true}
		if p.IsDisabled {
			pt.Disabled = "disabled"
		}
		rd.Pending = append(rd.Pending, pt)
	}
	return rd
}

// portOf returns the port of "host:port", or 0.
func portOf(addr string) int {
	i := strings.LastIndexByte(addr, ':')
	if i < 0 {
		return 0
	}
	n, err := strconv.Atoi(addr[i+1:])
	if err != nil {
		return 0
	}
	return n
}

// fallsBack reports whether a v1 failure should be retried on the old endpoint.
//
// Yalnızca "bu uç bizi anlamadı" türündeki hatalar: yol yok, doğrulama,
// yetki kapsamı ya da beklenmeyen biçim. "fail" (ör. RequiresVerifiedAccount)
// GERÇEK bir yanıttır — eski uç aynı hesabı aynı gerekçeyle reddeder; 429'da
// ikinci istek yalnızca sınırı büyütür.
func fallsBack(err error) bool {
	var ae *APIError
	if !errors.As(err, &ae) {
		return false
	}
	switch ae.Kind {
	case KindAuth, KindPathNotFound, KindValidation, KindShape:
		return true
	}
	return false
}

// pickErr chooses which of the two endpoint errors to show the user.
//
// Eski uç "yok"/"anlaşılmadı" dediyse asıl bilgi v1'in hatasındadır; aksi
// hâlde eski ucun yanıtı daha özgüldür (ör. v1 kapsam hatası, eski uç
// RequiresVerifiedAccount).
func pickErr(v1, old error) error {
	var ae *APIError
	if errors.As(old, &ae) && (ae.Kind == KindPathNotFound || ae.Kind == KindShape) {
		return v1
	}
	return old
}

// RunData fetches the agent's tunnels, preferring the v1 endpoint.
func (c *PlayitAPI) RunData() (*PlayitRunData, error) {
	var v1 rundataV1
	err := c.call("/v1/agents/rundata", struct{}{}, &v1)
	if err == nil {
		if v1.AgentID != "" {
			return v1.convert(), nil
		}
		err = &APIError{Kind: KindShape, Path: "/v1/agents/rundata",
			Detail: "agent_id yok"}
	}
	if !fallsBack(err) {
		return nil, err
	}
	var old rundataOld
	err2 := c.call("/agents/rundata", struct{}{}, &old)
	if err2 == nil {
		if old.AgentID != "" {
			return old.convert(), nil
		}
		err2 = &APIError{Kind: KindShape, Path: "/agents/rundata", Detail: "agent_id yok"}
	}
	return nil, pickErr(err, err2)
}

// ── Tünel oluşturma ─────────────────────────────────────────────────────────

// maxTunnelName keeps names well inside playit's limit (TunnelNameTooLong).
const maxTunnelName = 30

// TunnelName makes a playit-safe tunnel name from a server name.
//
// playit ASCII olmayan adı reddeder (TunnelNameIsNotAscii). Sunucu adları
// Türkçe olabiliyor ("Şövalye Dünyası"); harfleri ASCII karşılığına çevirip
// kalanları atıyoruz ki istek adı yüzünden düşmesin.
func TunnelName(s string) string {
	r := strings.NewReplacer("ç", "c", "Ç", "C", "ğ", "g", "Ğ", "G", "ı", "i",
		"İ", "I", "ö", "o", "Ö", "O", "ş", "s", "Ş", "S", "ü", "u", "Ü", "U")
	s = r.Replace(s)
	var b strings.Builder
	space := false
	for _, ch := range s {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9',
			ch == '-', ch == '_', ch == '.':
			b.WriteRune(ch)
			space = false
		case ch == ' ' || ch == '\t':
			if b.Len() > 0 && !space {
				b.WriteByte(' ')
				space = true
			}
		}
		if b.Len() >= maxTunnelName {
			break
		}
	}
	out := strings.TrimSpace(b.String())
	if len(out) > maxTunnelName {
		out = strings.TrimSpace(out[:maxTunnelName])
	}
	if out == "" {
		out = "MCOS Minecraft"
	}
	return out
}

type createV1Field struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type createV1Req struct {
	Ports struct {
		Type    string `json:"type"`
		Details string `json:"details"`
	} `json:"ports"`
	Origin struct {
		Type string `json:"type"`
		Data struct {
			AgentID string `json:"agent_id"`
			Config  struct {
				Fields []createV1Field `json:"fields"`
			} `json:"config"`
		} `json:"data"`
	} `json:"origin"`
	Enabled    bool    `json:"enabled"`
	Alloc      *string `json:"alloc"`
	Name       string  `json:"name"`
	FirewallID *string `json:"firewall_id"`
}

type createOldReq struct {
	Name       string `json:"name"`
	TunnelType string `json:"tunnel_type"`
	PortType   string `json:"port_type"`
	PortCount  int    `json:"port_count"`
	Origin     struct {
		Type string `json:"type"`
		Data struct {
			AgentID   string `json:"agent_id"`
			LocalIP   string `json:"local_ip"`
			LocalPort int    `json:"local_port"`
		} `json:"data"`
	} `json:"origin"`
	Enabled       bool    `json:"enabled"`
	Alloc         *string `json:"alloc"`
	FirewallID    *string `json:"firewall_id"`
	ProxyProtocol *string `json:"proxy_protocol"`
}

// CreateMinecraftTunnel asks playit for a Java tunnel to 127.0.0.1:localPort.
func (c *PlayitAPI) CreateMinecraftTunnel(agentID string, localPort int, name string) (string, error) {
	if agentID == "" {
		return "", &APIError{Kind: KindFail, Code: "InvalidAgentId", Path: "/v1/tunnels/create"}
	}
	name = TunnelName(name)

	var v1 createV1Req
	v1.Ports.Type = "tunnel-type"
	v1.Ports.Details = TunnelTypeMinecraftJava
	v1.Origin.Type = "agent"
	v1.Origin.Data.AgentID = agentID
	v1.Origin.Data.Config.Fields = []createV1Field{
		{Name: "local_ip", Value: "127.0.0.1"},
		{Name: "local_port", Value: strconv.Itoa(localPort)},
	}
	v1.Enabled = true
	v1.Name = name

	var res struct {
		ID string `json:"id"`
	}
	err := c.call("/v1/tunnels/create", v1, &res)
	if err == nil {
		if res.ID != "" {
			return res.ID, nil
		}
		err = &APIError{Kind: KindShape, Path: "/v1/tunnels/create", Detail: "id yok"}
	}
	if !fallsBack(err) {
		return "", err
	}

	var old createOldReq
	old.Name = name
	old.TunnelType = TunnelTypeMinecraftJava
	old.PortType = "tcp"
	old.PortCount = 1
	old.Origin.Type = "agent"
	old.Origin.Data.AgentID = agentID
	old.Origin.Data.LocalIP = "127.0.0.1"
	old.Origin.Data.LocalPort = localPort
	old.Enabled = true
	res.ID = ""
	err2 := c.call("/tunnels/create", old, &res)
	if err2 == nil {
		if res.ID != "" {
			return res.ID, nil
		}
		err2 = &APIError{Kind: KindShape, Path: "/tunnels/create", Detail: "id yok"}
	}
	return "", pickErr(err, err2)
}

// FindMinecraftTunnel looks for an existing tunnel serving localPort.
//
// Sıra: (1) bilinen kimlik, (2) yerel portu tutan Java tüneli, (3) yerel portu
// tutan genel TCP tüneli (kullanıcı sitede elle açmış olabilir — ikincisini
// açmak ücretsiz hesabın tünel sınırını boşuna harcar), (4) aynı adla
// BEKLEYEN Java tüneli (bekleyen tünelde yerel port bilgisi yok).
//
// Bilinen kimlik de PORTLA doğrulanır: kimlik başka bir yerel porta giden
// tünelinse (manifest başka sunucudan kopyalandı ya da kimlik bekleyen
// varsayılan tünelden kapıldı) o tünel bu sunucunun değildir. Doğrulamasız
// hâlde 25570'teki sunucu 25565'e giden adresi "genel adresim" diye gösteriyor
// ve kendisi için hiç tünel açılmıyordu (TestFindKnownIDOnOtherPortAndNamelessPending).
// Adsız aramada (name == "") bekleyenler ada göre eşlenmez: TunnelName("")
// "MCOS Minecraft" döner ve sunucusuz açılan varsayılan tünelle çakışırdı.
func FindMinecraftTunnel(rd *PlayitRunData, localPort int, name, knownID string) (PlayitTunnel, bool) {
	if rd == nil {
		return PlayitTunnel{}, false
	}
	if knownID != "" {
		for _, t := range rd.Tunnels {
			if t.ID == knownID && (t.LocalPort == 0 || t.LocalPort == localPort) {
				return t, true
			}
		}
		for _, t := range rd.Pending {
			if t.ID == knownID {
				return t, true
			}
		}
	}
	for _, t := range rd.Tunnels {
		if t.TunnelType == TunnelTypeMinecraftJava && t.LocalPort == localPort {
			return t, true
		}
	}
	for _, t := range rd.Tunnels {
		if t.TunnelType == "" && t.LocalPort == localPort && t.PortType != "udp" {
			return t, true
		}
	}
	if strings.TrimSpace(name) == "" {
		return PlayitTunnel{}, false
	}
	want := TunnelName(name)
	for _, t := range rd.Pending {
		if t.TunnelType == TunnelTypeMinecraftJava && t.Name == want {
			return t, true
		}
	}
	return PlayitTunnel{}, false
}

// EnsureResult is what EnsureMinecraftTunnel found or made.
type EnsureResult struct {
	Tunnel  PlayitTunnel
	Created bool
	RunData *PlayitRunData
}

// EnsureMinecraftTunnel makes sure a Java tunnel to localPort exists.
//
// İDEMPOTENT: tünel zaten varsa (ya da bekliyorsa) yenisi AÇILMAZ. Panelde
// "Tünel oluştur/yenile"ye iki kez basmak iki tünel açsaydı, ücretsiz hesabın
// sınırı birkaç denemede dolardı.
func (c *PlayitAPI) EnsureMinecraftTunnel(localPort int, name string) (EnsureResult, error) {
	rd, err := c.RunData()
	if err != nil {
		return EnsureResult{}, err
	}
	return c.EnsureMinecraftTunnelIn(rd, localPort, name, "")
}

// EnsureMinecraftTunnelIn is EnsureMinecraftTunnel on already-fetched rundata.
//
// Arka plan eşitleyicisi her turda rundata'yı BİR KEZ okur ve bütün sunucular
// için bunu kullanır: sunucu başına ayrı okuma 429 riskini katlardı.
func (c *PlayitAPI) EnsureMinecraftTunnelIn(rd *PlayitRunData, localPort int, name, knownID string) (EnsureResult, error) {
	if t, ok := FindMinecraftTunnel(rd, localPort, name, knownID); ok {
		return EnsureResult{Tunnel: t, RunData: rd}, nil
	}
	agentID := ""
	if rd != nil {
		agentID = rd.AgentID
	}
	id, err := c.CreateMinecraftTunnel(agentID, localPort, name)
	if err != nil {
		return EnsureResult{RunData: rd}, err
	}
	return EnsureResult{
		Tunnel: PlayitTunnel{ID: id, Name: TunnelName(name),
			TunnelType: TunnelTypeMinecraftJava, PortType: "tcp",
			LocalIP: "127.0.0.1", LocalPort: localPort, Pending: true},
		Created: true,
		RunData: rd,
	}, nil
}
