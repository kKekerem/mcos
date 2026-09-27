package ipcclient

import "mcos/internal/ipc"

// ServerPerfPack starts installing/updating a server's performance pack.
// Hemen döner; iş daemon'da arka planda sürer (bkz. ipc.MethodServerPerfPack).
func (cl *Client) ServerPerfPack(id string) (ipc.ServerPerfPackResult, error) {
	var res ipc.ServerPerfPackResult
	err := cl.call(ipc.MethodServerPerfPack, ipc.ServerPerfPackParams{ID: id}, &res)
	return res, err
}
