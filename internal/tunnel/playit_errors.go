package tunnel

import (
	"strconv"
	"strings"
)

// ════════════════════════════════════════════════════════════════════════════
// playit HATALARININ TÜRKÇESİ
// ════════════════════════════════════════════════════════════════════════════
//
// playit enum adları ("RequiresVerifiedAccount", "InvalidAgentKey") panele
// olduğu gibi düşseydi kullanıcı ne yapacağını bilemezdi. Her birini "ne oldu
// + ne yapmalı" cümlesine çeviriyoruz. Adlar resmî playit-agent kaynağındaki
// TunnelCreateError(V1), AuthError ve playit-cli 1.0.10 ikilisindeki dizgelerle
// (AccountDoesNotExist, AgentNotSelfManaged, …) birebir aynıdır.

const (
	msgVerifyEmail = "playit hesabınızın e-postasını doğrulayın (playit.gg → hesap)"
	msgPremium     = "Bu seçenek playit Premium ister"
	msgRelink      = "anahtar geçersiz; hesabı yeniden bağlayın"
	msgExpired     = "anahtarın süresi doldu; hesabı yeniden bağlayın"
	msgGuest       = "misafir hesap: playit.gg'de giriş yapın/hesabı kaydedin"
	msgAgentGone   = "ajan playit hesabında bulunamadı; hesabı yeniden bağlayın"
	msgAgentOld    = "playit ajanı çok eski; MCOS'u güncelleyin"
	// ManualHint, otomatik açma olmadığında kullanıcının sitede ne yapacağını
	// TAM olarak söyler: tür + yerel adres. Eşitleyici sitede açılan tüneli
	// yerel portundan tanır ve adresi yine kendisi gösterir.
	msgManual   = "tüneli playit.gg sitesinden açın: tür Minecraft Java, yerel adres 127.0.0.1:25565"
	msgRejected = "playit tünel ayarlarını reddetti"
)

// playitCodeTR maps playit enum names to user-facing Turkish.
var playitCodeTR = map[string]string{
	// ── TunnelCreateError / TunnelCreateErrorV1 ("fail") ─────────────────
	"RequiresVerifiedAccount":         msgVerifyEmail,
	"RequiresPlayitPremium":           msgPremium,
	"RegionRequiresPlayitPremium":     msgPremium + " (seçilen bölge)",
	"PublicPortRequiresPlayitPremium": msgPremium + " (sabit genel port)",
	"AgentNotFound":                   msgAgentGone,
	"InvalidAgentId":                  msgAgentGone,
	"ManagedMissingAgentId":           msgAgentGone,
	"AgentVersionTooOld":              msgAgentOld,
	"DefaultAgentNotSupported":        "bu ajan türü tünel açamıyor; " + msgManual,
	"InvalidTunnelName":               "tünel adı geçersiz; sunucu adını kısaltın",
	"TunnelNameIsNotAscii":            "tünel adında Türkçe/özel karakter olamaz",
	"TunnelNameTooLong":               "tünel adı çok uzun; sunucu adını kısaltın",
	"RegionNotSupported":              "seçilen playit bölgesi desteklenmiyor",
	"FirewallNotFound":                msgRejected + " (güvenlik duvarı bulunamadı)",
	"DedicatedIpNotFound":             msgRejected + " (özel IP bulunamadı)",
	"DedicatedIpPortNotAvailable":     msgRejected + " (özel IP'de port dolu)",
	"DedicatedIpNotEnoughSpace":       msgRejected + " (özel IP'de yer yok)",
	"PortAllocNotFound":               msgRejected + " (port ayırması yok)",
	"PortAllocCurrentlyAssigned":      msgRejected + " (port başka tünelde)",
	"PortAllocDoesNotMatchPortDetails": msgRejected +
		" (port ayırması türle uyuşmuyor)",
	"AllocRequestNotSupportedByPorts": msgRejected + " (ayırma türle uyuşmuyor)",
	"AllocInvalid":                    msgRejected + " (geçersiz ayırma)",
	"InvalidOrigin":                   msgRejected + " (geçersiz hedef)",
	"InvalidTunnelConfig":             msgRejected + " (geçersiz yerel adres)",
	"InvalidPortCount":                msgRejected + " (geçersiz port sayısı)",
	"InvalidIpHostname":               msgRejected + " (geçersiz alan adı)",
	"InvalidHostnameId":               msgRejected + " (geçersiz alan adı)",
	"HostnameHasTunnelTypeTarget":     msgRejected + " (alan adı başka tünele bağlı)",
	// playitd'nin IPC dizgelerinden: hesap ajan sınırını aşınca ajan durur.
	"AgentDisabledOverLimit": "hesap ajan sınırını aştı; playit.gg'de eski ajanları silin",

	// ── AuthError ("error" / type "auth") ─────────────────────────────────
	"AuthRequired":                      msgRelink,
	"InvalidAgentKey":                   msgRelink,
	"InvalidApiKey":                     msgRelink,
	"AccountDoesNotExist":               "playit hesabı bulunamadı; hesabı yeniden bağlayın",
	"SessionExpired":                    msgExpired,
	"NoLongerValid":                     msgExpired,
	"InvalidHeader":                     "playit isteği tanımadı (geçersiz başlık); MCOS'u güncelleyin",
	"InvalidSignature":                  "playit imzayı reddetti; hesabı yeniden bağlayın",
	"InvalidTimestamp":                  "sistem saati yanlış; saat eşitlemesini bekleyip yeniden deneyin",
	"InvalidAuthType":                   "bu işlem ajan anahtarıyla yapılamıyor; " + msgManual,
	"ScopeNotAllowed":                   "bu işlem ajan anahtarıyla yapılamıyor; " + msgManual,
	"AgentNotSelfManaged":               "bu ajan türü tünel açamıyor; " + msgManual,
	"SelfManagedAgentCanOnlyAffectSelf": "ajan yalnızca kendi tünellerini yönetebilir; " + msgManual,
	"DefaultAgentBlocked":               "bu ajan türü engelli; " + msgManual,
	"GuestAccountNotAllowed":            msgGuest,
	"EmailMustBeVerified":               msgVerifyEmail,
	"AccountNotAuthorized":              "playit hesabının bu işleme yetkisi yok",
	"AdminOnly":                         "playit hesabının bu işleme yetkisi yok",
	"NotAllowedWithReadOnly":            "playit hesabı salt okunur; playit.gg'de izinleri denetleyin",
	"TotpRequred":                       "hesapta iki adımlı doğrulama gerekli; playit.gg'de giriş yapın",

	"TooManyRequests": "playit çok sık istek aldı; biraz sonra kendiliğinden yeniden denenecek",
}

// PlayitMessage returns the Turkish explanation of an API error.
func PlayitMessage(e *APIError) string {
	if e == nil {
		return ""
	}
	if m, ok := playitCodeTR[e.Code]; ok {
		return m
	}
	switch e.Kind {
	case KindRateLimit:
		return playitCodeTR["TooManyRequests"]
	case KindNetwork:
		return "playit.gg'ye ulaşılamadı (internet bağlantısını denetleyin)"
	case KindInternal:
		return "playit sunucusunda iç hata; biraz sonra kendiliğinden yeniden denenecek"
	case KindPathNotFound:
		return "playit API yolu bulunamadı (" + e.Detail + "); MCOS'u güncelleyin"
	case KindValidation:
		return "playit isteği reddetti: " + e.Detail
	case KindHTTP:
		return "playit beklenmeyen yanıt verdi (HTTP " + strconv.Itoa(e.HTTPStatus) + ")"
	case KindShape:
		return "playit yanıtı anlaşılamadı; MCOS'u güncelleyin"
	case KindAuth:
		return "playit yetki hatası (" + e.Code + "); hesabı yeniden bağlayın"
	case KindFail:
		if e.Code != "" {
			return "playit isteği reddetti (" + e.Code + ")"
		}
	}
	if e.Code != "" {
		return "playit: " + e.Code
	}
	return "playit: bilinmeyen hata"
}

// MessageForPort is err's Turkish message with the manual-setup hint naming
// the server's own local port.
//
// msgManual "127.0.0.1:25565" der; 25570'teki bir sunucu için bu ipucu
// kullanıcıya YANLIŞ porta tünel açtırırdı ve eşitleyici o tüneli yerel
// portundan tanımadığı için sunucu yine tünelsiz kalırdı.
func MessageForPort(err error, port int) string {
	if err == nil {
		return ""
	}
	m := err.Error()
	if port > 0 && port != 25565 {
		m = strings.ReplaceAll(m, "127.0.0.1:25565", "127.0.0.1:"+strconv.Itoa(port))
	}
	return m
}

// AccountMessage explains a non-normal account_status, or "".
func AccountMessage(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "guest":
		return msgGuest
	case "email-not-verified":
		return msgVerifyEmail
	case "banned":
		return "playit hesabı askıya alınmış; playit.gg'de ayrıntıya bakın"
	}
	return ""
}

// Messages lists what the user must know about the account: the guest /
// unverified state and the dashboard's Critical notices.
//
// Yalnızca "Critical" duyurular: playit sitesi bilgi amaçlı duyuruları da
// buraya koyuyor; hepsini panelde göstermek asıl hatayı gömerdi.
func (rd *PlayitRunData) Messages() []string {
	if rd == nil {
		return nil
	}
	var out []string
	if m := AccountMessage(rd.AccountStatus); m != "" {
		out = append(out, m)
	}
	for _, n := range rd.Notices {
		if !strings.EqualFold(n.Priority, "critical") || strings.TrimSpace(n.Message) == "" {
			continue
		}
		m := strings.TrimSpace(n.Message)
		if l := strings.TrimSpace(n.ResolveLink); l != "" {
			m += " → " + l
		}
		out = append(out, m)
	}
	return out
}
