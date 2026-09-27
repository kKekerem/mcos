package ipcclient

import "mcos/internal/ipc"

// Otomatik yedek planı çağrıları. Ayrı dosyada: client.go başka özelliklerle
// paylaşılıyor.

// BackupPolicy returns a server's automatic backup plan and the next due time.
func (cl *Client) BackupPolicy(serverID string) (ipc.BackupPolicyResult, error) {
	var res ipc.BackupPolicyResult
	err := cl.call(ipc.MethodBackupPolicy, ipc.BackupPolicyParams{ServerID: serverID}, &res)
	return res, err
}

// SetBackupPolicy stores a new automatic backup plan. Geçersiz değerde
// daemon'un Türkçe hatası döner.
func (cl *Client) SetBackupPolicy(p ipc.BackupSetPolicyParams) (ipc.BackupPolicyResult, error) {
	var res ipc.BackupPolicyResult
	err := cl.call(ipc.MethodBackupSetPolicy, p, &res)
	return res, err
}
