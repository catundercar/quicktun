package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"go.uber.org/zap"
)

// bridge listens on 127.0.0.1:<random> and, for each accepted connection,
// dials the configured auth-proxy, sends an HTTP CONNECT preamble with the
// agent's Bearer token, then forwards bytes both directions.
//
// Used by Runtime so rathole-client's plain-TCP connections look like
// authenticated CONNECT tunnels to the auth-proxy.
type bridge struct {
	authProxyAddr string
	bearerToken   string
	lg            *zap.Logger

	listener net.Listener
	wg       sync.WaitGroup
}

// startBridge listens on 127.0.0.1:0 and runs the accept loop in a goroutine.
// Returns the bridge with its bound local address available via localAddr().
// Closes when ctx is done; close() blocks until all in-flight connections
// finish.
func startBridge(ctx context.Context, authProxyAddr, bearer string, lg *zap.Logger) (*bridge, error) {
	if authProxyAddr == "" {
		return nil, fmt.Errorf("agent: empty auth_proxy_endpoint")
	}
	if lg == nil {
		lg = zap.NewNop()
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("agent: bridge listen: %w", err)
	}
	b := &bridge{
		authProxyAddr: authProxyAddr,
		bearerToken:   bearer,
		lg:            lg,
		listener:      lis,
	}
	go b.serve(ctx)
	return b, nil
}

func (b *bridge) localAddr() string { return b.listener.Addr().String() }

// close stops the listener + waits for in-flight handlers to drain.
func (b *bridge) close() {
	_ = b.listener.Close()
	b.wg.Wait()
}

func (b *bridge) serve(ctx context.Context) {
	go func() {
		<-ctx.Done()
		_ = b.listener.Close()
	}()
	for {
		conn, err := b.listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			b.lg.Warn("agent bridge: accept", zap.Error(err))
			continue
		}
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			defer conn.Close()
			b.handle(ctx, conn)
		}()
	}
}

func (b *bridge) handle(ctx context.Context, local net.Conn) {
	upstream, err := net.DialTimeout("tcp", b.authProxyAddr, 5*time.Second)
	if err != nil {
		b.lg.Warn("agent bridge: dial auth-proxy",
			zap.String("addr", b.authProxyAddr), zap.Error(err))
		return
	}
	defer upstream.Close()

	// CONNECT preamble. Host is dummy — auth-proxy ignores it; routing is
	// keyed by the Bearer token.
	host, _, _ := net.SplitHostPort(b.authProxyAddr)
	if host == "" {
		host = "relay"
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodConnect,
		(&url.URL{Host: host}).String(), nil)
	req.Host = host + ":443"
	req.Header.Set("Authorization", "Bearer "+b.bearerToken)
	if err := req.Write(upstream); err != nil {
		b.lg.Warn("agent bridge: write CONNECT", zap.Error(err))
		return
	}

	br := bufio.NewReader(upstream)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		b.lg.Warn("agent bridge: read CONNECT response", zap.Error(err))
		return
	}
	if resp.StatusCode != http.StatusOK {
		b.lg.Warn("agent bridge: auth-proxy rejected",
			zap.Int("status", resp.StatusCode))
		return
	}

	// Flush any bytes buffered after the 200 OK response.
	if br.Buffered() > 0 {
		buf, _ := br.Peek(br.Buffered())
		_, _ = local.Write(buf)
	}

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstream, local); done <- struct{}{} }()
	go func() { _, _ = io.Copy(local, upstream); done <- struct{}{} }()
	<-done
}
