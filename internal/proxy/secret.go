package proxy

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
)

// secretLen, modern yönlendirme anahtarının uzunluğu.
const secretLen = 32

const secretAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// LoadOrCreateSecret returns the forwarding secret stored at path, creating a
// fresh one if missing.
//
// KALICI: anahtar değişirse eşlerdeki sunucular yeni anahtarı alıp yeniden
// başlayana kadar proxy'den gelen oyuncuları reddeder. Yalnızca harf ve rakam:
// anahtar paper-global.yml'e ve TOML'a tırnak içinde yazılıyor.
func LoadOrCreateSecret(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		if s := strings.TrimSpace(string(b)); len(s) >= 16 {
			return s, nil
		}
	}
	max := big.NewInt(int64(len(secretAlphabet)))
	out := make([]byte, secretLen)
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("proxy anahtarı üretilemedi: %w", err)
		}
		out[i] = secretAlphabet[n.Int64()]
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	// Satır sonu YOK: Velocity dosyanın tamamını anahtar sayar.
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return "", err
	}
	return string(out), nil
}
