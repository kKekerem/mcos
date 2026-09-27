package ipcclient

import "mcos/internal/ipc"

// ── Sistem güncellemesi (USB'deki yeni ISO) ─────────────────────────────────

// UpdateScan lists MCOS ISOs on attached USB drives.
//
// Kendi bağlantısında koşar (callLong): her bölüm ve her ISO bağlanıp
// okunur, büyük bir USB'de onlarca saniye sürebilir; paylaşılan bağlantıda
// bu süre boyunca panel donardı.
func (cl *Client) UpdateScan() (ipc.UpdateScanResult, error) {
	var res ipc.UpdateScanResult
	err := cl.callLong(ipc.MethodSystemUpdateScan, struct{}{}, &res)
	return res, err
}

// UpdateStart starts the update in the background and returns at once.
func (cl *Client) UpdateStart(dev, path string) error {
	var res ipc.OKResult
	return cl.call(ipc.MethodSystemUpdate, ipc.UpdateParams{Device: dev, Path: path}, &res)
}

// UpdateStatus returns the progress/result of the last update.
func (cl *Client) UpdateStatus() (ipc.UpdateStatusResult, error) {
	var res ipc.UpdateStatusResult
	err := cl.call(ipc.MethodSystemUpdateStatus, struct{}{}, &res)
	return res, err
}
