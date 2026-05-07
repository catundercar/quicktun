package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
)

// RenderRatholeClient produces a rathole-client TOML config from a Bootstrap
// response. The token field is sha256_hex(rawToken), per
// docs/07-token-contract.md (the server side renders the same hex into each
// service block — the agent must do the same so rathole's shared-secret
// handshake succeeds).
//
// Service keys MATCH the server-side rendering in internal/relay/render.go:
// "<site_slug>__<service_slug>". rathole correlates client and server
// services by these keys.
//
// Output is deterministic: tunnels are sorted by ServiceSlug.
func RenderRatholeClient(boot *quicktunv1.BootstrapResponse, rawToken string) (string, error) {
	if boot == nil {
		return "", fmt.Errorf("agent: nil bootstrap response")
	}
	if boot.GetAuthProxyEndpoint() == "" {
		return "", fmt.Errorf("agent: missing auth_proxy_endpoint")
	}
	if rawToken == "" {
		return "", fmt.Errorf("agent: empty token")
	}

	sum := sha256.Sum256([]byte(rawToken))
	tokenHex := hex.EncodeToString(sum[:])

	// Stable order by ServiceSlug for deterministic rendering / diffs.
	tunnels := make([]*quicktunv1.TunnelBinding, 0, len(boot.GetTunnels()))
	for _, t := range boot.GetTunnels() {
		if t == nil {
			continue
		}
		tunnels = append(tunnels, t)
	}
	sort.SliceStable(tunnels, func(i, j int) bool {
		return tunnels[i].GetServiceSlug() < tunnels[j].GetServiceSlug()
	})

	var b strings.Builder
	fmt.Fprintf(&b, "# quicktun-agent rendered config for %s\n", boot.GetSiteName())
	b.WriteString("# DO NOT EDIT MANUALLY — overwritten on every bootstrap.\n\n")

	b.WriteString("[client]\n")
	fmt.Fprintf(&b, "remote_addr = %q\n", boot.GetAuthProxyEndpoint())
	fmt.Fprintf(&b, "default_token = %q\n\n", tokenHex)

	for _, t := range tunnels {
		name := boot.GetSiteSlug() + "__" + t.GetServiceSlug()
		fmt.Fprintf(&b, "[client.services.%s]\n", name)
		fmt.Fprintf(&b, "local_addr = %q\n\n",
			fmt.Sprintf("%s:%d", t.GetTargetAddr(), t.GetTargetPort()))
	}

	return b.String(), nil
}
