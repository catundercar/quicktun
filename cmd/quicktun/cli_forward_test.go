package main

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// startMockAuthProxy is a minimal CONNECT echo proxy for handleForward tests.
// It validates the Bearer token and the CONNECT target authority; on success
// it answers 200 OK and echoes subsequent bytes back to the client.
//
// Mirrors the operator-session contract enforced by
// internal/authproxy/router.go (target must equal the configured service
// loopback addr) so the test catches regressions in either layer.
func startMockAuthProxy(t *testing.T, expectedToken, expectedTarget string) (string, func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var wg sync.WaitGroup

	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				br := bufio.NewReader(conn)
				req, err := http.ReadRequest(br)
				if err != nil {
					return
				}
				if req.Method != http.MethodConnect {
					_, _ = conn.Write([]byte("HTTP/1.1 405 Method Not Allowed\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
					return
				}
				auth := req.Header.Get("Authorization")
				token := ""
				if parts := strings.SplitN(auth, " ", 2); len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
					token = parts[1]
				}
				if token != expectedToken {
					_, _ = conn.Write([]byte("HTTP/1.1 401 Unauthorized\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
					return
				}
				if req.Host != expectedTarget {
					_, _ = conn.Write([]byte("HTTP/1.1 401 Unauthorized\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
					return
				}
				_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
				// Echo any subsequent bytes back to the client.
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()

	cleanup := func() {
		_ = lis.Close()
		wg.Wait()
	}
	return lis.Addr().String(), cleanup
}

// TestHandleForwardEcho exercises handleForward against a fake auth-proxy:
// good token + correct target authority must yield a bidirectional pipe.
func TestHandleForwardEcho(t *testing.T) {
	const target = "127.0.0.1:9999"
	proxyAddr, stop := startMockAuthProxy(t, "good-token", target)
	defer stop()

	// Use net.Pipe to simulate the local side. handleForward closes upstream
	// on return; the local side is owned by the caller (the Accept loop in
	// production), matching the ownership we mimic here.
	cli, srv := net.Pipe()
	defer cli.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handleForward(context.Background(), srv, proxyAddr, "good-token", target)
	}()

	_, err := cli.Write([]byte("ping"))
	require.NoError(t, err)
	buf := make([]byte, 4)
	require.NoError(t, cli.SetReadDeadline(time.Now().Add(2*time.Second)))
	n, err := io.ReadFull(cli, buf)
	require.NoError(t, err)
	require.Equal(t, "ping", string(buf[:n]))

	// Closing the local side ends both io.Copy goroutines so handleForward
	// returns and the test doesn't leak.
	_ = cli.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleForward did not return after local close")
	}
}

// TestHandleForwardRejectedToken: a wrong bearer token causes the auth-proxy
// to write 401; handleForward must close upstream and return without
// forwarding bytes.
func TestHandleForwardRejectedToken(t *testing.T) {
	const target = "127.0.0.1:9999"
	proxyAddr, stop := startMockAuthProxy(t, "expected-token", target)
	defer stop()

	cli, srv := net.Pipe()
	defer cli.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handleForward(context.Background(), srv, proxyAddr, "wrong-token", target)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleForward did not return after rejected CONNECT")
	}
}
