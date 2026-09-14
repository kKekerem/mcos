package remote

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Bu dosya, uzaktan kontrol için kullanılan TLS sertifikasını üretir ve saklar.
//
// ════════════════════════════════════════════════════════════════════════════
// NEDEN TLS ŞART
// ════════════════════════════════════════════════════════════════════════════
//
// Telefon uygulaması her istekte bir jeton (token) gönderir. Düz HTTP ile bu
// jeton ağda AÇIK metin olarak gider: aynı Wi-Fi'deki herkes onu okuyup
// sunucunun tam denetimini ele geçirebilir. Kullanıcı bu bağlantıyı playit
// üzerinden internete de açabilir — orada "aynı ağdaki herkes" tüm internet
// demektir.
//
// ── Neden kendinden imzalı ──────────────────────────────────────────────────
// Gerçek bir sertifika için alan adı ve bir sertifika otoritesi gerekir. Bu
// cihazın alan adı yok; ev ağında bir IP adresi var. Kendinden imzalı bir
// sertifika şifrelemeyi sağlar, ama "bu gerçekten benim sunucum mu?" sorusunu
// yanıtlamaz.
//
// O soruyu PARMAK İZİ yanıtlıyor: panel sertifikanın SHA-256 parmak izini
// gösterir, uygulama ilk bağlantıda onu kaydeder ve sonraki bağlantılarda
// değişirse reddeder (TOFU — ilk kullanımda güven). Araya giren biri kendi
// sertifikasını sunmak zorunda kalır ve parmak izi tutmaz.

const (
	certFileName = "cert.pem"
	keyFileName  = "key.pem"

	// certValidity, sertifikanın ömrü.
	//
	// 10 yıl: bu bir ev sunucusu. Sertifikanın sessizce süresi dolup uzaktan
	// erişimin bir sabah çalışmaz hâle gelmesi, kullanıcının anlayamayacağı
	// bir arıza olurdu. Güvenlik anahtar uzunluğundan geliyor, kısa ömürden
	// değil.
	certValidity = 10 * 365 * 24 * time.Hour
)

// certificate is the TLS identity plus its fingerprint.
type certificate struct {
	tls         tls.Certificate
	fingerprint string // SHA-256, iki nokta ile ayrılmış onaltılık
}

// loadOrCreateCert returns the stored certificate, creating one if needed.
//
// dir: sertifikanın saklandığı klasör. Kalıcı olmalı — her açılışta yeni
// sertifika üretmek, telefondaki parmak izini her seferinde geçersiz kılar ve
// kullanıcıya "araya giren var" uyarısı gösterirdi.
func loadOrCreateCert(dir string) (*certificate, error) {
	certPath := filepath.Join(dir, certFileName)
	keyPath := filepath.Join(dir, keyFileName)

	if c, err := loadCert(certPath, keyPath); err == nil {
		return c, nil
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("sertifika klasörü oluşturulamadı: %w", err)
	}
	if err := createCert(certPath, keyPath); err != nil {
		return nil, err
	}
	return loadCert(certPath, keyPath)
}

func loadCert(certPath, keyPath string) (*certificate, error) {
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	if len(pair.Certificate) == 0 {
		return nil, fmt.Errorf("sertifika boş")
	}
	return &certificate{
		tls:         pair,
		fingerprint: fingerprintOf(pair.Certificate[0]),
	}, nil
}

// fingerprintOf returns the SHA-256 of the DER bytes, colon separated.
//
// Bu, tarayıcıların ve openssl'in gösterdiği biçimin AYNISI: kullanıcı
// istediğinde başka bir araçla doğrulayabilmeli.
func fingerprintOf(der []byte) string {
	sum := sha256.Sum256(der)
	hexStr := strings.ToUpper(hex.EncodeToString(sum[:]))
	var b strings.Builder
	for i := 0; i < len(hexStr); i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(hexStr[i : i+2])
	}
	return b.String()
}

func createCert(certPath, keyPath string) error {
	// P-256: her telefon ve her TLS kitaplığı destekler, RSA'dan çok daha
	// hızlı anahtar üretir (bu cihaz yavaş bir mini PC olabilir).
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("anahtar üretilemedi: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("seri numarası üretilemedi: %w", err)
	}

	now := time.Now()
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "MCOS",
			Organization: []string{"MCOS"},
		},
		// Saat geriye kaymış olabilir (RTC pili bitik bir mini PC): bir saat
		// geriden başlatmak, "henüz geçerli değil" hatasını önler.
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(certValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	// Makinenin BÜTÜN adresleri sertifikaya yazılır: kullanıcı hangi
	// arabirimden bağlanacağını bilmiyoruz (kablo, Wi-Fi, VPN) ve adres
	// eşleşmezse bazı istemciler bağlantıyı reddeder.
	tmpl.IPAddresses = append(tmpl.IPAddresses, net.IPv4(127, 0, 0, 1), net.IPv6loopback)
	tmpl.DNSNames = append(tmpl.DNSNames, "localhost")
	if host, err := os.Hostname(); err == nil && host != "" {
		tmpl.DNSNames = append(tmpl.DNSNames, host)
	}
	for _, ip := range localIPs() {
		tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("sertifika üretilemedi: %w", err)
	}

	if err := writePEM(certPath, "CERTIFICATE", der, 0o644); err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("anahtar kodlanamadı: %w", err)
	}
	// 0600: özel anahtar yalnızca kök tarafından okunabilmeli.
	return writePEM(keyPath, "EC PRIVATE KEY", keyDER, 0o600)
}

func writePEM(path, blockType string, der []byte, perm os.FileMode) error {
	buf := pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
	if buf == nil {
		return fmt.Errorf("%s PEM kodlanamadı", blockType)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// localIPs lists this machine's own addresses.
func localIPs() []net.IP {
	var out []net.IP
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.IsLoopback() {
			continue
		}
		if ip4 := ipn.IP.To4(); ip4 != nil {
			out = append(out, ip4)
		}
	}
	return out
}
