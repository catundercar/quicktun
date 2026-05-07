package authproxy_test

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/tulip/quicktun/internal/authproxy"
)

// startEchoBackend listens on 127.0.0.1:0 and echoes every received byte
// back. Returns its addr and a cleanup func.
func startEchoBackend(t *testing.T) (string, func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var wg sync.WaitGroup
	go func() {
		for {
			c, err := lis.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer c.Close()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	return lis.Addr().String(), func() {
		_ = lis.Close()
		wg.Wait()
	}
}

func startProxy(t *testing.T, route authproxy.RouteFunc) (string, context.CancelFunc) {
	t.Helper()
	s := authproxy.New(route, zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- s.Serve(ctx, "127.0.0.1:0") }()
	// Wait for listener to bind.
	require.Eventually(t, func() bool { return s.Addr() != nil }, 1*time.Second, 10*time.Millisecond)
	return s.Addr().String(), func() { cancel(); <-errCh }
}

func TestServerForwardsAfterValidConnect(t *testing.T) {
	backend, stopBackend := startEchoBackend(t)
	defer stopBackend()

	proxyAddr, stopProxy := startProxy(t, func(_ context.Context, raw string) (string, error) {
		if raw != "good-token" {
			return "", authproxy.ErrUnauthenticated
		}
		return backend, nil
	})
	defer stopProxy()

	c, err := net.Dial("tcp", proxyAddr)
	require.NoError(t, err)
	defer c.Close()
	_, err = c.Write([]byte("CONNECT relay:443 HTTP/1.1\r\nHost: relay:443\r\nAuthorization: Bearer good-token\r\n\r\n"))
	require.NoError(t, err)

	// Read 200 OK status line.
	br := bufio.NewReader(c)
	statusLine, err := br.ReadString('\n')
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(statusLine, "HTTP/1.1 200"), "got: %s", statusLine)
	// Drain headers until blank line.
	for {
		line, err := br.ReadString('\n')
		require.NoError(t, err)
		if line == "\r\n" || line == "\n" {
			break
		}
	}

	// Now bidirectional. Write hello, expect echo.
	_, err = c.Write([]byte("hello-tunnel"))
	require.NoError(t, err)
	buf := make([]byte, 12)
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := io.ReadFull(br, buf)
	require.NoError(t, err)
	require.Equal(t, "hello-tunnel", string(buf[:n]))
}

func TestServerRejectsBadMethod(t *testing.T) {
	proxyAddr, stop := startProxy(t, func(_ context.Context, _ string) (string, error) {
		return "", authproxy.ErrUnauthenticated
	})
	defer stop()

	c, err := net.Dial("tcp", proxyAddr)
	require.NoError(t, err)
	defer c.Close()
	_, _ = c.Write([]byte("GET / HTTP/1.1\r\nHost: relay\r\n\r\n"))
	statusLine, err := bufio.NewReader(c).ReadString('\n')
	require.NoError(t, err)
	require.Contains(t, statusLine, "405")
}

func TestServerRejectsMissingBearer(t *testing.T) {
	proxyAddr, stop := startProxy(t, func(_ context.Context, raw string) (string, error) {
		if raw == "" {
			return "", authproxy.ErrUnauthenticated
		}
		return "127.0.0.1:1", nil
	})
	defer stop()

	c, err := net.Dial("tcp", proxyAddr)
	require.NoError(t, err)
	defer c.Close()
	_, _ = c.Write([]byte("CONNECT relay:443 HTTP/1.1\r\nHost: relay:443\r\n\r\n"))
	statusLine, err := bufio.NewReader(c).ReadString('\n')
	require.NoError(t, err)
	require.Contains(t, statusLine, "401")
}

func TestServerReturns502OnBackendDial(t *testing.T) {
	proxyAddr, stop := startProxy(t, func(_ context.Context, _ string) (string, error) {
		return "127.0.0.1:1", nil // unlikely-to-be-listening port
	})
	defer stop()

	c, err := net.Dial("tcp", proxyAddr)
	require.NoError(t, err)
	defer c.Close()
	_, _ = c.Write([]byte("CONNECT relay:443 HTTP/1.1\r\nHost: relay:443\r\nAuthorization: Bearer x\r\n\r\n"))
	statusLine, err := bufio.NewReader(c).ReadString('\n')
	require.NoError(t, err)
	require.Contains(t, statusLine, "502")
}

func TestServerForwardsBufferedClientBytes(t *testing.T) {
	// Write CONNECT preamble + trailing app bytes in ONE Write. Backend should
	// see the trailing bytes after preamble.
	backend, stopBackend := startEchoBackend(t)
	defer stopBackend()
	proxyAddr, stop := startProxy(t, func(_ context.Context, _ string) (string, error) {
		return backend, nil
	})
	defer stop()

	c, err := net.Dial("tcp", proxyAddr)
	require.NoError(t, err)
	defer c.Close()
	_, _ = c.Write([]byte("CONNECT relay:443 HTTP/1.1\r\nHost: relay:443\r\nAuthorization: Bearer x\r\n\r\nIMMEDIATE-PAYLOAD"))

	br := bufio.NewReader(c)
	// Drain status + headers
	for {
		line, err := br.ReadString('\n')
		require.NoError(t, err)
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	// Read echoed payload
	buf := make([]byte, len("IMMEDIATE-PAYLOAD"))
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = io.ReadFull(br, buf)
	require.NoError(t, err)
	require.Equal(t, "IMMEDIATE-PAYLOAD", string(buf))
}
