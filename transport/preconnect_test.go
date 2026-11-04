package transport

import (
	"context"
	"net"
	"testing"
	"time"
)

func preconnect(t *testing.T, tr *Transport, s *h2Server) {
	t.Helper()
	host, port, err := net.SplitHostPort(s.addr)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := tr.Preconnect(ctx, host, port); err != nil {
		t.Fatalf("Preconnect: %v\n%s", err, s.dump())
	}
}

// pooled is the one connection the H2 transport holds for the server.
func pooled(t *testing.T, tr *Transport, s *h2Server) *persistentConn {
	t.Helper()
	h2 := tr.GetHTTP2Transport()
	h2.connsMu.RLock()
	defer h2.connsMu.RUnlock()
	conn, ok := h2.conns[s.addr]
	if !ok {
		t.Fatalf("no pooled connection for %s", s.addr)
	}
	return conn
}

// A preconnected connection is the one the next request goes out on, and a
// second preconnect while it is usable opens nothing.
func TestPreconnectedConnCarriesTheRequest(t *testing.T) {
	s := startH2Server(t, h2Config{})
	tr := retryTransport(t)

	preconnect(t, tr, s)
	preconnect(t, tr, s)
	if n := s.connCount(); n != 1 {
		t.Fatalf("server saw %d connections after two preconnects, want 1", n)
	}
	if n := len(s.attempts()); n != 0 {
		t.Fatalf("preconnect sent %d requests, want none", n)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := tr.Do(ctx, &Request{Method: "GET", URL: s.url("/")})
	if err != nil {
		t.Fatalf("GET: %v\n%s", err, s.dump())
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if n := s.connCount(); n != 1 {
		t.Fatalf("server saw %d connections, want the preconnected one only", n)
	}
}

// Past the default idle limit a connection is redialled; SetH2ConnLimits keeps
// it in service.
func TestH2ConnLimitsKeepAnIdleConn(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit time.Duration
		conns int
	}{
		{"default limits redial", 0, 2},
		{"raised limits reuse", time.Hour, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := startH2Server(t, h2Config{})
			tr := retryTransport(t)
			tr.SetH2ConnLimits(tc.limit, tc.limit)

			preconnect(t, tr, s)
			conn := pooled(t, tr, s)
			conn.mu.Lock()
			conn.createdAt = conn.createdAt.Add(-10 * time.Minute)
			conn.lastUsedAt = conn.lastUsedAt.Add(-10 * time.Minute)
			conn.mu.Unlock()

			preconnect(t, tr, s)
			if n := s.connCount(); n != tc.conns {
				t.Fatalf("server saw %d connections, want %d", n, tc.conns)
			}
		})
	}
}

// A connection the peer has closed is replaced, not handed out again.
func TestPreconnectReplacesAClosedConn(t *testing.T) {
	s := startH2Server(t, h2Config{})
	tr := retryTransport(t)
	tr.SetH2ConnLimits(time.Hour, time.Hour)

	preconnect(t, tr, s)
	conn := pooled(t, tr, s)
	conn.tlsConn.Close()

	deadline := time.Now().Add(5 * time.Second)
	for !connClosing(conn) {
		if time.Now().After(deadline) {
			t.Fatal("the closed connection never reported closing")
		}
		time.Sleep(10 * time.Millisecond)
	}

	preconnect(t, tr, s)
	if n := s.connCount(); n != 2 {
		t.Fatalf("server saw %d connections, want a second one replacing the closed", n)
	}
	if pooled(t, tr, s) == conn {
		t.Fatal("the closed connection is still the pooled one")
	}
}
