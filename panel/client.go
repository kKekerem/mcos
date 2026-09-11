// Package panel implements the rich MCOS TUI (Bubble Tea + Lip Gloss). It is a
// thin client over the mcosd JSON-RPC API: every screen reflects daemon state
// and every action is an RPC call.
package panel

import "mcos/internal/ipcclient"

// Client is the shared daemon client.
//
// Govde internal/ipcclient icine tasindi ki framebuffer paneli de ayni
// cagrilari Bubble Tea bagimliligi olmadan kullanabilsin. Bu takma ad, mevcut
// ekran kodunun tek satirini bile degistirmeden calismasini saglar.
type Client = ipcclient.Client

// Dial connects to mcosd at endpoint.
var Dial = ipcclient.Dial
