package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
)

// newForwardCmd builds the `quicktun forward <service-name> --local-port N`
// command: a local TCP listener that bridges through the auth-proxy to the
// named service's relay port using the operator's session token.
//
// Each accepted local connection opens its own CONNECT tunnel — keeping the
// CLI stateless beyond credentials and letting many parallel sessions share
// the same listener (e.g. multiple `ssh` invocations against
// 127.0.0.1:<localPort>).
func newForwardCmd() *cobra.Command {
	var (
		localPort int
		// bind address default: 127.0.0.1 — never bind all interfaces by
		// default so a forgotten `forward` doesn't expose a backend service
		// to the operator's local network.
		bindAddr string
	)
	cmd := &cobra.Command{
		Use:   "forward <service-name>",
		Short: "Forward a local TCP port to a service via the auth-proxy.",
		Long: `Open a local TCP listener that tunnels each connection through the
auth-proxy to the named service's relay port. The service name accepts either
the canonical 6-segment form (projects/p/sites/s/services/svc) or the
3-segment shortcut (p/s/svc).

Example:
  quicktun forward clinic-network/bastion-1/ssh --local-port 2222
  ssh -p 2222 user@127.0.0.1
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := canonicalServiceName(args[0])
			if err != nil {
				return err
			}

			creds, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck
			if creds.AuthProxyEndpoint == "" {
				return fmt.Errorf("credentials missing auth_proxy_endpoint; re-run `quicktun login --auth-proxy <addr>`")
			}

			// Resolve the service's relay port up front. We do this once per
			// `forward` invocation rather than per-connection so a typo or
			// permission error fails fast (instead of after the operator
			// points `ssh` at the listener and waits).
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			svcClient := quicktunv1.NewServiceServiceClient(conn)
			svc, err := svcClient.GetService(ctx, &quicktunv1.GetServiceRequest{Name: name})
			cancel()
			if err != nil {
				return fmt.Errorf("get service: %w", err)
			}
			if svc.GetRelayPort() == 0 {
				return fmt.Errorf("service %s has no allocated relay port", name)
			}

			// auth-proxy enforces target == "127.0.0.1:<svcPort>" for operator
			// session tokens (see internal/authproxy/router.go::routeOperator).
			target := fmt.Sprintf("127.0.0.1:%d", svc.GetRelayPort())
			listenAddr := bindAddr + ":" + strconv.Itoa(localPort)

			lis, err := net.Listen("tcp", listenAddr)
			if err != nil {
				return fmt.Errorf("listen: %w", err)
			}
			defer lis.Close() //nolint:errcheck
			fmt.Fprintf(os.Stderr, "Forwarding %s -> %s (auth-proxy: %s)\n",
				lis.Addr().String(), name, creds.AuthProxyEndpoint)

			// Watch SIGINT/SIGTERM and tear down the listener so the Accept
			// loop returns; the in-flight handler goroutines drain via wg.
			sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
			defer stop()
			go func() {
				<-sigCtx.Done()
				_ = lis.Close()
			}()

			var wg sync.WaitGroup
			for {
				local, err := lis.Accept()
				if err != nil {
					if sigCtx.Err() != nil {
						wg.Wait()
						fmt.Fprintln(os.Stderr, "Forwarding stopped.")
						return nil
					}
					fmt.Fprintf(os.Stderr, "accept: %v\n", err)
					continue
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					defer local.Close() //nolint:errcheck
					handleForward(sigCtx, local, creds.AuthProxyEndpoint, creds.SessionToken, target)
				}()
			}
		},
	}
	cmd.Flags().IntVarP(&localPort, "local-port", "p", 0, "local TCP port (0 = random)")
	cmd.Flags().StringVar(&bindAddr, "bind", "127.0.0.1", "local bind address")
	return cmd
}

// handleForward dials the auth-proxy, sends a CONNECT preamble carrying the
// operator's Bearer token + the service relay-port target, then bidirectionally
// io.Copy's between local and the resulting tunnel until either side closes.
//
// Mirrors internal/agent/bridge.go's handler in spirit but with two key
// differences: (1) target is the SERVICE relay port (auth-proxy uses target to
// pick the per-service backend), (2) the bearer is the operator session token.
//
// We deliberately don't share code with internal/agent/bridge.go yet — that
// package is private to agent, the duplication is small, and Plan 9 is too
// shallow to justify a connectbridge package for two ~30-line functions.
func handleForward(ctx context.Context, local net.Conn, proxyAddr, token, target string) {
	upstream, err := net.DialTimeout("tcp", proxyAddr, 5*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "forward: dial auth-proxy %s: %v\n", proxyAddr, err)
		return
	}
	defer upstream.Close() //nolint:errcheck

	// CONNECT request — req.Host carries the target authority that
	// authproxy.Router.Route reads for operator-session routing.
	req, _ := http.NewRequestWithContext(ctx, http.MethodConnect,
		(&url.URL{Host: target}).String(), nil)
	req.Host = target
	req.Header.Set("Authorization", "Bearer "+token)
	if err := req.Write(upstream); err != nil {
		fmt.Fprintf(os.Stderr, "forward: write CONNECT: %v\n", err)
		return
	}

	br := bufio.NewReader(upstream)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "forward: read CONNECT response: %v\n", err)
		return
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "forward: auth-proxy rejected (%d)\n", resp.StatusCode)
		return
	}

	// Flush any bytes buffered after the 200 OK response — the auth-proxy
	// can start streaming backend data immediately after the status line.
	if br.Buffered() > 0 {
		buf, _ := br.Peek(br.Buffered())
		_, _ = local.Write(buf)
	}

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstream, local); done <- struct{}{} }()
	go func() { _, _ = io.Copy(local, upstream); done <- struct{}{} }()
	<-done
}
