package main

import (
	"context"
	"crypto/tls"
	"fmt"

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
