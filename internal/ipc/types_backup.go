package ipc

import (
	"time"

	"mcos/internal/model"
)

// Otomatik yedek planı: okuma ve ayarlama. Ayrı dosyada, çünkü types.go ve
// protocol.go başka özelliklerle paylaşılıyor; yedek planının sözleşmesi
// burada kendi başına okunabilsin.

const (
	// MethodBackupPolicy returns a server's automatic backup plan and when the
	// next automatic backup is due.
	MethodBackupPolicy = "backup.policy"
	// MethodBackupSetPolicy stores a new plan (doğrulanır; geçersiz değer
	// Türkçe bir hatayla, CodeInvalidParams ile reddedilir).
	MethodBackupSetPolicy = "backup.setPolicy"
)

// BackupPolicyParams selects the server of backup.policy.
type BackupPolicyParams struct {
	ServerID string `json:"serverId"`
}

// BackupSetPolicyParams is the new plan of backup.setPolicy.
//
// Auto=false planı SİLMEZ: Schedule ve Keep saklanır, yeniden açan kullanıcı
// eski seçimini bulur. Boş Schedule / nil Keep = mevcut değer kalsın.
type BackupSetPolicyParams struct {
	ServerID string `json:"serverId"`
	Auto     bool   `json:"auto"`
	// Schedule: "6h", "1d", "2d@04:00", "7d@03:30" (bkz. model.ParseBackupSchedule).
	Schedule string `json:"schedule,omitempty"`
	// Keep: saklanacak kopya; 0 = sınırsız, nil = değiştirme.
	Keep *int `json:"keep,omitempty"`
}

// BackupPolicyResult is the plan plus the scheduler's view of it.
type BackupPolicyResult struct {
	ServerID string             `json:"serverId"`
	Policy   model.BackupPolicy `json:"policy"`
	// Summary: "Her 6 saatte bir · son 5 kopya" ya da "Kapalı". Uygulama ve
	// panel metni kendileri kurmasın diye hazır gelir.
	Summary string `json:"summary"`
	// Next: bir sonraki otomatik yedek, SİSTEMİN saat diliminde (JSON'daki
	// ofsetle). Gösteren taraf kendi saat dilimine çevirmeden HH:MM'yi yazar.
	// Otomatik yedek kapalıysa ya da saat güvenilmezse yok.
	Next *time.Time `json:"next,omitempty"`
	// Due: yedek şimdi alınmalı (Next geçmişte); Reason nedenini söyler.
	Due    bool   `json:"due,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Running: bu sunucunun otomatik yedeği şu an alınıyor.
	Running bool `json:"running,omitempty"`
	// Last: zamanlamanın dayandığı en son yedek.
	Last *time.Time `json:"last,omitempty"`
	// Timezone: planın saatlerinin yorumlandığı IANA dilimi (boş = sistem).
	Timezone string `json:"timezone,omitempty"`
	// Problem: plan uygulanamıyorsa Türkçe neden (bozuk kayıt, yanlış saat).
	Problem string `json:"problem,omitempty"`
}
