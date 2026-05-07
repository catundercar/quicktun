package main

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/tulip/quicktun/internal/clicred"
)

// dialControl returns a gRPC client conn to creds.Endpoint. If
// creds.SessionToken is non-empty, every RPC carries
// `Authorization: Bearer <token>`. tls_insecure=true uses plaintext
// (dev only) and lets the bearer credential ride a non-TLS transport.
func dialControl(creds *clicred.Credentials) (*grpc.ClientConn, error) {
	if creds == nil || creds.Endpoint == "" {
		return nil, fmt.Errorf("quicktun: missing endpoint; run `quicktun login` first")
	}

	var transport credentials.TransportCredentials
	if creds.TLSInsecure {
		transport = insecure.NewCredentials()
	} else {
		// TLS 1.3 is the secure default; matches internal/agent/runtime.go.
		transport = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13})
	}

	opts := []grpc.DialOption{grpc.WithTransportCredentials(transport)}
	if creds.SessionToken != "" {
		opts = append(opts, grpc.WithPerRPCCredentials(bearerCreds{
			token:    creds.SessionToken,
			insecure: creds.TLSInsecure,
		}))
	}
	return grpc.NewClient(creds.Endpoint, opts...)
}

// bearerCreds is a tiny PerRPCCredentials that injects a static Bearer
// token. We can't reuse the agent's bearer type because it lives in
// internal/agent and we want the CLI binary to depend only on
// clicred + the proto package.
type bearerCreds struct {
	token    string
	insecure bool
}

func (b bearerCreds) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + b.token}, nil
}

func (b bearerCreds) RequireTransportSecurity() bool { return !b.insecure }

// loadAndDial reads creds from --config (or default path), dials gRPC,
// and returns both. Caller MUST defer conn.Close(). The returned
// *clicred.Credentials lets caller-side code that wants the operator's
// email or auth-proxy endpoint avoid a second Load.
//
// On a missing credentials file we wrap the error with a hint so the
// operator knows to run `quicktun login` rather than chasing a generic
// "no such file" message.
func loadAndDial(cmd *cobra.Command) (*clicred.Credentials, *grpc.ClientConn, error) {
	path, _ := cmd.Root().PersistentFlags().GetString("config")
	if path == "" {
		var err error
		path, err = clicred.DefaultPath()
		if err != nil {
			return nil, nil, err
		}
	}
	creds, err := clicred.Load(path)
	if err != nil {
		return nil, nil, fmt.Errorf("load credentials: %w (run `quicktun login` first)", err)
	}
	conn, err := dialControl(creds)
	if err != nil {
		return nil, nil, err
	}
	return creds, conn, nil
}
