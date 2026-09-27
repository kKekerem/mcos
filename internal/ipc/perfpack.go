package ipc

// Performans paketi çağrıları. Ayrı dosyada: protocol.go ve types.go başka
// özelliklerle aynı anda düzenleniyor.

// MethodServerPerfPack installs/updates a server's performance pack.
//
// HEMEN döner, iş daemon'da arka planda sürer: mod indirmek dakikalar
// sürebilir, panelin RPC zaman aşımı ise 10 sn. Bitiş, sunucu kaydındaki
// PerfPack.At değişince anlaşılır.
const MethodServerPerfPack = "server.perfPack"

// ServerPerfPackParams names the server.
type ServerPerfPackParams struct {
	ID string `json:"id"`
}

// ServerPerfPackResult says whether the background job started.
type ServerPerfPackResult struct {
	// Started: iş başladı. false ise aynı sunucuda bir paket işi zaten
	// sürüyordur (Message açıklar).
	Started bool   `json:"started"`
	Message string `json:"message,omitempty"`
	// Items, paketin bu sunucu için içeriğidir (panelde gösterilir).
	Items []string `json:"items,omitempty"`
}
