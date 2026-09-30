package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
)

// Client is a synchronous JSON-RPC client over a single persistent connection.
// Calls are serialized so the simple one-request-per-line framing stays correct.
type Client struct {
	conn     net.Conn
	reader   *bufio.Reader
	endpoint string // set when created via DialClient; enables auto-reconnect
	mu       sync.Mutex
	nextID   int64
}

// NewClient wraps an existing connection.
func NewClient(conn net.Conn) *Client {
	return &Client{conn: conn, reader: bufio.NewReaderSize(conn, 64*1024)}
}

// DialClient connects to endpoint and returns a ready client. Clients created
// this way transparently reconnect if the daemon restarts mid-session.
func DialClient(endpoint string) (*Client, error) {
	conn, err := Dial(endpoint)
	if err != nil {
		return nil, err
	}
	c := NewClient(conn)
	c.endpoint = endpoint
	return c, nil
}

// Close closes the underlying connection.
func (c *Client) Close() error { return c.conn.Close() }

// reconnect re-dials the endpoint (caller holds the lock).
func (c *Client) reconnect() error {
	if c.endpoint == "" {
		return fmt.Errorf("ipc: no endpoint to reconnect")
	}
	conn, err := Dial(c.endpoint)
	if err != nil {
		return err
	}
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.conn = conn
	c.reader = bufio.NewReaderSize(conn, 64*1024)
	return nil
}

// Call sends a request and decodes the result into out (which may be nil). On a
// transport failure it reconnects once and retries, so a daemon restart does not
// kill the front-end.
func (c *Client) Call(method string, params any, out any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := atomic.AddInt64(&c.nextID, 1)
	req := Request{JSONRPC: Version, ID: id, Method: method}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("ipc: encode params: %w", err)
		}
		req.Params = raw
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("ipc: encode request: %w", err)
	}
	payload = append(payload, '\n')

	resp, err := c.roundtrip(payload)
	if err != nil && c.endpoint != "" {
		if rcErr := c.reconnect(); rcErr == nil {
			resp, err = c.roundtrip(payload)
		}
	}
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return resp.Error
	}
	if out != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("ipc: decode result: %w", err)
		}
	}
	return nil
}

// roundtrip writes one request and reads one response (caller holds the lock).
func (c *Client) roundtrip(payload []byte) (Response, error) {
	if _, err := c.conn.Write(payload); err != nil {
		return Response{}, fmt.Errorf("ipc: write: %w", err)
	}
	line, err := readLine(c.reader)
	if err != nil {
		return Response{}, fmt.Errorf("ipc: read: %w", err)
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return Response{}, fmt.Errorf("ipc: decode response: %w", err)
	}
	return resp, nil
}
