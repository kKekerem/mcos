package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"mcos/internal/log"
)

// Handler processes a decoded request and returns a result to be JSON-encoded,
// or an error. Returning an *Error sets the JSON-RPC error verbatim; any other
// error becomes CodeInternalError.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// Server is a JSON-RPC server over a single listener. It dispatches one request
// per line per connection and supports many concurrent connections.
type Server struct {
	ln       net.Listener
	log      *log.Logger
	mu       sync.RWMutex
	handlers map[string]Handler

	connMu sync.Mutex
	conns  map[net.Conn]struct{}
	closed bool
}

// NewServer creates a server bound to ln.
func NewServer(ln net.Listener, lg *log.Logger) *Server {
	return &Server{
		ln:       ln,
		log:      lg,
		handlers: map[string]Handler{},
		conns:    map[net.Conn]struct{}{},
	}
}

// Handle registers a handler for a method, replacing any existing one.
func (s *Server) Handle(method string, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = h
}

// Serve accepts connections until the listener is closed or ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		s.Close()
	}()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			s.connMu.Lock()
			closed := s.closed
			s.connMu.Unlock()
			if closed || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		s.trackConn(conn, true)
		go s.serveConn(ctx, conn)
	}
}

func (s *Server) trackConn(c net.Conn, add bool) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if add {
		s.conns[c] = struct{}{}
	} else {
		delete(s.conns, c)
	}
}

// Close stops accepting and closes all live connections.
func (s *Server) Close() {
	s.connMu.Lock()
	if s.closed {
		s.connMu.Unlock()
		return
	}
	s.closed = true
	for c := range s.conns {
		_ = c.Close()
	}
	s.connMu.Unlock()
	_ = s.ln.Close()
}

func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	defer func() {
		_ = conn.Close()
		s.trackConn(conn, false)
	}()
	reader := bufio.NewReaderSize(conn, 64*1024)
	writer := bufio.NewWriter(conn)
	enc := json.NewEncoder(writer)
	for {
		line, err := readLine(reader)
		if err != nil {
			if err != io.EOF && s.log != nil {
				s.log.Debugf("ipc: read: %v", err)
			}
			return
		}
		if len(line) == 0 {
			continue
		}
		resp := s.dispatch(ctx, line)
		if err := enc.Encode(resp); err != nil {
			return
		}
		if err := writer.Flush(); err != nil {
			return
		}
	}
}

func (s *Server) dispatch(ctx context.Context, line []byte) Response {
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return Response{JSONRPC: Version, Error: &Error{Code: CodeParseError, Message: "invalid JSON"}}
	}
	resp := Response{JSONRPC: Version, ID: req.ID}
	s.mu.RLock()
	h, ok := s.handlers[req.Method]
	s.mu.RUnlock()
	if !ok {
		resp.Error = &Error{Code: CodeMethodNotFound, Message: "method not found: " + req.Method}
		return resp
	}
	result, err := s.call(ctx, h, req.Method, req.Params)
	if err != nil {
		var rpcErr *Error
		if errors.As(err, &rpcErr) {
			resp.Error = rpcErr
		} else {
			resp.Error = &Error{Code: CodeInternalError, Message: err.Error()}
		}
		return resp
	}
	if result != nil {
		raw, err := json.Marshal(result)
		if err != nil {
			resp.Error = &Error{Code: CodeInternalError, Message: "encode result: " + err.Error()}
			return resp
		}
		resp.Result = raw
	}
	return resp
}

// call invokes a handler with panic recovery so a single misbehaving handler
// returns an error to the client instead of crashing the whole daemon.
func (s *Server) call(ctx context.Context, h Handler, method string, params json.RawMessage) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			if s.log != nil {
				s.log.Errorf("ipc: handler %q panicked: %v", method, r)
			}
			result = nil
			err = &Error{Code: CodeInternalError, Message: fmt.Sprintf("internal error in %s", method)}
		}
	}()
	return h(ctx, params)
}

// readLine reads one newline-terminated message, returning it without the
// trailing newline.
func readLine(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadBytes('\n')
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return line, err
}
