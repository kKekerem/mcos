package ipcclient

import "mcos/internal/ipc"

// Bu dosya UZAKTAN KONTROL ve SSH çağrılarını sarmalar.
//
// Panelin gösterdiği jeton, adres ve parmak izi buradan geliyor; telefon
// uygulaması olmadan bu değerleri görmenin başka yolu yok.

// RemoteStatus mirrors the daemon's remote-control state.
type RemoteStatus struct {
	Enabled     bool     `json:"enabled"`
	Running     bool     `json:"running"`
	Port        int      `json:"port"`
	Token       string   `json:"token,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	Addresses   []string `json:"addresses,omitempty"`
	URL         string   `json:"url,omitempty"`
	Note        string   `json:"note,omitempty"`
}

// RemoteStatus fetches the bridge state.
func (cl *Client) RemoteStatus() (RemoteStatus, error) {
	var res RemoteStatus
	err := cl.call(ipc.MethodRemoteStatus, nil, &res)
	return res, err
}

// RemoteEnable turns the bridge on, generating a token if there is none.
//
// port 0 ise daemon varsayılanı (2223) kullanır.
func (cl *Client) RemoteEnable(port int) (RemoteStatus, error) {
	var res RemoteStatus
	params := map[string]any{}
	if port > 0 {
		params["port"] = port
	}
	err := cl.call(ipc.MethodRemoteEnable, params, &res)
	return res, err
}

// RemoteDisable stops the bridge.
func (cl *Client) RemoteDisable() (RemoteStatus, error) {
	var res RemoteStatus
	err := cl.call(ipc.MethodRemoteDisable, nil, &res)
	return res, err
}

// RemoteRotate issues a new token, cutting off every connected phone.
func (cl *Client) RemoteRotate() (RemoteStatus, error) {
	var res RemoteStatus
	err := cl.call(ipc.MethodRemoteRotate, nil, &res)
	return res, err
}

// ── SSH ─────────────────────────────────────────────────────────────────────

// SSHStatus mirrors the daemon's shell-access state.
type SSHStatus struct {
	Available   bool     `json:"available"`
	Running     bool     `json:"running"`
	Enabled     bool     `json:"enabled"`
	Port        int      `json:"port"`
	PasswordSet bool     `json:"passwordSet"`
	Keys        int      `json:"keys"`
	User        string   `json:"user"`
	Flavor      string   `json:"flavor,omitempty"`
	Addresses   []string `json:"addresses,omitempty"`
	Note        string   `json:"note,omitempty"`
}

// SSHStatus fetches the SSH server state.
func (cl *Client) SSHStatus() (SSHStatus, error) {
	var res SSHStatus
	err := cl.call(ipc.MethodSSHStatus, nil, &res)
	return res, err
}

// SSHEnable starts the SSH server. port 0 uses the default (22).
func (cl *Client) SSHEnable(port int) (SSHStatus, error) {
	var res SSHStatus
	params := map[string]any{}
	if port > 0 {
		params["port"] = port
	}
	err := cl.call(ipc.MethodSSHEnable, params, &res)
	return res, err
}

// SSHDisable stops the SSH server.
func (cl *Client) SSHDisable() (SSHStatus, error) {
	var res SSHStatus
	err := cl.call(ipc.MethodSSHDisable, nil, &res)
	return res, err
}

// SSHSetPassword sets the root login password.
func (cl *Client) SSHSetPassword(password string) (SSHStatus, error) {
	var res SSHStatus
	err := cl.call(ipc.MethodSSHPassword,
		map[string]string{"password": password}, &res)
	return res, err
}

// SSHAddKey installs a public key for password-less login.
func (cl *Client) SSHAddKey(key string) (SSHStatus, error) {
	var res SSHStatus
	err := cl.call(ipc.MethodSSHAddKey, map[string]string{"key": key}, &res)
	return res, err
}
