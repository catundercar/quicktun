package agent_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
	"github.com/tulip/quicktun/internal/agent"
)

func TestRenderRatholeClientWithTunnels(t *testing.T) {
	const raw = "raw-secret-token"
	sum := sha256.Sum256([]byte(raw))
	wantHex := hex.EncodeToString(sum[:])

	boot := &quicktunv1.BootstrapResponse{
		SiteName:          "projects/proj/sites/bastion-1",
		ProjectSlug:       "proj",
		SiteSlug:          "bastion-1",
		AuthProxyEndpoint: "relay.test:20000",
		Tunnels: []*quicktunv1.TunnelBinding{
			{ServiceSlug: "ssh", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: "tcp", RelayPort: 20001},
			{ServiceSlug: "web", TargetAddr: "10.0.0.5", TargetPort: 80, Proto: "tcp", RelayPort: 20002},
		},
	}

	out, err := agent.RenderRatholeClient(boot, raw)
	require.NoError(t, err)

	// [client] header + remote_addr + sha256-hex token.
	require.Contains(t, out, "[client]")
	require.Contains(t, out, `remote_addr = "relay.test:20000"`)
	require.Contains(t, out, `default_token = "`+wantHex+`"`)

	// Service block keys are <site_slug>__<service_slug> (matches server-side).
	require.Contains(t, out, "[client.services.bastion-1__ssh]")
	require.Contains(t, out, "[client.services.bastion-1__web]")

	// local_addr binds to TargetAddr:TargetPort.
	require.Contains(t, out, `local_addr = "127.0.0.1:22"`)
	require.Contains(t, out, `local_addr = "10.0.0.5:80"`)

	// Deterministic order — ssh before web alphabetically.
	idxSSH := strings.Index(out, "bastion-1__ssh")
	idxWeb := strings.Index(out, "bastion-1__web")
	require.NotEqual(t, -1, idxSSH)
	require.NotEqual(t, -1, idxWeb)
	require.Less(t, idxSSH, idxWeb)
}

func TestRenderRatholeClientNoTunnels(t *testing.T) {
	boot := &quicktunv1.BootstrapResponse{
		SiteName:          "projects/proj/sites/bastion-1",
		SiteSlug:          "bastion-1",
		AuthProxyEndpoint: "relay.test:20000",
	}
	out, err := agent.RenderRatholeClient(boot, "tok")
	require.NoError(t, err)
	require.Contains(t, out, "[client]")
	require.Contains(t, out, `remote_addr = "relay.test:20000"`)
	require.NotContains(t, out, "[client.services.")
}

func TestRenderRatholeClientRejectsEmptyControlAddr(t *testing.T) {
	_, err := agent.RenderRatholeClient(&quicktunv1.BootstrapResponse{}, "tok")
	require.Error(t, err)
	require.Contains(t, err.Error(), "auth_proxy_endpoint")
}

func TestRenderRatholeClientRejectsEmptyToken(t *testing.T) {
	_, err := agent.RenderRatholeClient(&quicktunv1.BootstrapResponse{
		AuthProxyEndpoint: "relay.test:20000",
	}, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "token")
}

func TestRenderRatholeClientRejectsNilBoot(t *testing.T) {
	_, err := agent.RenderRatholeClient(nil, "tok")
	require.Error(t, err)
}

func TestRenderRatholeClientEscapesUntrustedFields(t *testing.T) {
	// A target_addr containing a double-quote must NOT escape the TOML
	// string boundary. fmt's %q semantics escape it.
	boot := &quicktunv1.BootstrapResponse{
		SiteSlug:          "site",
		AuthProxyEndpoint: "relay.test:20000",
		Tunnels: []*quicktunv1.TunnelBinding{
			{ServiceSlug: "evil", TargetAddr: `bad"addr`, TargetPort: 1234},
		},
	}
	out, err := agent.RenderRatholeClient(boot, "tok")
	require.NoError(t, err)
	// The literal quote must appear escaped (\") inside the TOML string.
	require.Contains(t, out, `local_addr = "bad\"addr:1234"`)
	require.NotContains(t, out, `local_addr = "bad"addr:1234"`)
}
