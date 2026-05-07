package authproxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/tulip/quicktun/internal/metrics"
)

// RouteFunc resolves a token + CONNECT target to a backend addr. The Router
// type satisfies this; tests use stub closures. target is the request-URI
// authority from the CONNECT line ("host:port"). It is used by the operator
// session path for per-service routing; the site agent path ignores it.
type RouteFunc func(ctx context.Context, rawToken, target string) (string, error)

// Server is a single CONNECT-protocol auth gateway.
type Server struct {
	route   RouteFunc
	lg      *zap.Logger
	metrics *metrics.AuthProxyMetrics // may be nil; helpers are nil-safe

	// listener is set once Serve has bound. Stored via atomic.Pointer so
	// Addr() (called from other goroutines, e.g. tests) can read it
	// race-free against Serve's write.
	listener atomic.Pointer[net.Listener]
	wg       sync.WaitGroup
}

// New constructs a Server that authenticates via the given RouteFunc.
// metrics may be nil — every helper on *metrics.AuthProxyMetrics is nil-safe.
func New(route RouteFunc, lg *zap.Logger, m *metrics.AuthProxyMetrics) *Server {
	if lg == nil {
		lg = zap.NewNop()
	}
	return &Server{route: route, lg: lg, metrics: m}
}

// Serve listens on addr and serves until ctx is cancelled. Blocks.
func (s *Server) Serve(ctx context.Context, addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("authproxy: listen %s: %w", addr, err)
	}
	s.listener.Store(&lis)
	s.lg.Info("authproxy: listening", zap.String("addr", lis.Addr().String()))

	go func() {
		<-ctx.Done()
		_ = lis.Close()
	}()

	for {
		conn, err := lis.Accept()
		if err != nil {
			if ctx.Err() != nil {
				s.wg.Wait()
				return nil
			}
			s.lg.Warn("authproxy: accept", zap.Error(err))
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer conn.Close()
			s.handle(ctx, conn)
		}()
	}
}

// Addr returns the bound listener address. Useful for tests using port 0.
// Returns nil before Serve has bound.
func (s *Server) Addr() net.Addr {
	lp := s.listener.Load()
	if lp == nil {
		return nil
	}
	return (*lp).Addr()
}

func (s *Server) handle(ctx context.Context, conn net.Conn) {
	start := time.Now()
	// status holds the numeric HTTP status we ended up writing; defer
	// observes it once so each handle() invocation produces exactly one
	// metric sample. 0 means "client never finished a request line"
	// (e.g. malformed input, timeout) — emit as "000" so the dashboard
	// can spot it without polluting the standard {200,401,...} bucket.
	status := 0
	defer func() {
		label := "000"
		if status > 0 {
			label = strconv.Itoa(status)
		}
		s.metrics.ObserveConnect(label, time.Since(start).Seconds())
	}()

	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(conn)
	req, err := http.ReadRequest(br)
	if err != nil {
		s.lg.Debug("authproxy: read request", zap.Error(err))
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	if req.Method != http.MethodConnect {
		status = 405
		writeStatus(conn, "HTTP/1.1 405 Method Not Allowed", "method must be CONNECT")
		return
	}

	rawToken := bearerToken(req.Header.Get("Authorization"))
	backend, err := s.route(ctx, rawToken, req.Host)
	if err != nil {
		if errors.Is(err, ErrInternal) {
			s.lg.Warn("authproxy: routing internal error", zap.Error(err))
			status = 500
			writeStatus(conn, "HTTP/1.1 500 Internal Server Error", "")
			return
		}
		status = 401
		writeStatus(conn, "HTTP/1.1 401 Unauthorized", "")
		return
	}

	upstream, err := net.DialTimeout("tcp", backend, 5*time.Second)
	if err != nil {
		s.lg.Warn("authproxy: dial backend",
			zap.String("backend", backend), zap.Error(err))
		status = 502
		writeStatus(conn, "HTTP/1.1 502 Bad Gateway", "")
		return
	}
	defer upstream.Close()

	status = 200
	writeStatus(conn, "HTTP/1.1 200 OK", "")

	// Flush any bytes the client buffered after the CONNECT preamble.
	if br.Buffered() > 0 {
		buf, _ := br.Peek(br.Buffered())
		_, _ = upstream.Write(buf)
	}

	// Bidirectional copy. Bail when either direction closes.
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstream, conn); done <- struct{}{} }()
	go func() { _, _ = io.Copy(conn, upstream); done <- struct{}{} }()
	<-done
}

func writeStatus(conn net.Conn, status, body string) {
	var b strings.Builder
	b.WriteString(status + "\r\n")
	if body != "" {
		fmt.Fprintf(&b, "Content-Length: %d\r\nContent-Type: text/plain\r\n", len(body))
	} else {
		b.WriteString("Content-Length: 0\r\n")
	}
	b.WriteString("Connection: close\r\n\r\n")
	if body != "" {
		b.WriteString(body)
	}
	_, _ = conn.Write([]byte(b.String()))
}

func bearerToken(h string) string {
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}
