package agent

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
	"go.uber.org/zap"
)

// fakeProxy is a minimal CONNECT echo proxy for tests. It accepts any
// CONNECT, reads the Bearer token, validates against expectedToken (empty
// = accept any), responds 200 OK, then echoes bytes back. If
// expectedToken doesn't match it returns 401.
func startFakeProxy(t *testing.T, expectedToken string) (string, func()) {
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
				if expectedToken != "" && token != expectedToken {
					_, _ = conn.Write([]byte("HTTP/1.1 401 Unauthorized\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
					return
				}
				_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
				// Echo any subsequent bytes the bridge sends.
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

func TestBridgeForwardsThroughFakeProxy(t *testing.T) {
	proxyAddr, stopProxy := startFakeProxy(t, "good-token")
	defer stopProxy()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	br, err := startBridge(ctx, proxyAddr, "good-token", zap.NewNop())
	require.NoError(t, err)
	defer br.close()

	// Dial the bridge as if we were rathole-client.
	c, err := net.Dial("tcp", br.localAddr())
	require.NoError(t, err)
	defer c.Close()

	_, err = c.Write([]byte("ping-bytes"))
	require.NoError(t, err)

	buf := make([]byte, len("ping-bytes"))
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := io.ReadFull(c, buf)
	require.NoError(t, err)
	require.Equal(t, "ping-bytes", string(buf[:n]))
}

func TestBridgeClosesLocalConnOnAuthProxyReject(t *testing.T) {
	proxyAddr, stopProxy := startFakeProxy(t, "expected-token")
	defer stopProxy()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	br, err := startBridge(ctx, proxyAddr, "wrong-token", zap.NewNop())
	require.NoError(t, err)
	defer br.close()

	c, err := net.Dial("tcp", br.localAddr())
	require.NoError(t, err)
	defer c.Close()

	// The bridge dials, sends CONNECT, gets 401, gives up + closes upstream.
	// Local conn should see EOF on read.
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = io.ReadAll(c)
	require.NoError(t, err) // EOF maps to nil with ReadAll
	// Note: ReadAll returning empty slice + nil err means upstream closed
	// without sending anything, which is the bridge's behavior on reject.
}

func TestBridgeRejectsEmptyAuthProxyAddr(t *testing.T) {
	_, err := startBridge(context.Background(), "", "tok", zap.NewNop())
	require.Error(t, err)
}
