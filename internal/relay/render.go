// Package relay renders rathole-server config files and supervises the
// per-project rathole-server processes that terminate reverse tunnels.
package relay

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tulip/quicktun/internal/model"
	"github.com/tulip/quicktun/internal/resource"
)

// ServiceBinding is a flattened (site, service, agent_token) tuple ready for
// rendering into rathole's per-service config block.
type ServiceBinding struct {
	SiteSlug    string
	ServiceSlug string
	RelayPort   uint16
	AgentToken  string // hash is OK — agent stores the same hash
}

// RenderRatholeServer returns a TOML config for a per-project rathole-server.
//
// The control port (rathole's [server] bind_addr) is the LOWEST port in the
// project's relay_port_range. Service ports occupy the rest of the range.
// All binds are 127.0.0.1 so external traffic must go through the auth-proxy
// (Plan 8) to reach rathole.
//
// RenderRatholeServer sorts bindings in-place for deterministic output.
func RenderRatholeServer(p *model.Project, bindings []ServiceBinding) (string, error) {
	minP, _, err := resource.ParsePortRange(p.RelayPortRange)
	if err != nil {
		return "", fmt.Errorf("relay: %w", err)
	}

	sort.SliceStable(bindings, func(i, j int) bool {
		if bindings[i].SiteSlug != bindings[j].SiteSlug {
			return bindings[i].SiteSlug < bindings[j].SiteSlug
		}
		return bindings[i].ServiceSlug < bindings[j].ServiceSlug
	})

	var b strings.Builder
	fmt.Fprintf(&b, "# quicktun-rendered config for project %q (id=%d)\n",
		p.Slug, p.ID)
	fmt.Fprintf(&b, "# DO NOT EDIT MANUALLY — overwritten on next reconfigure.\n\n")

	b.WriteString("[server]\n")
	fmt.Fprintf(&b, "bind_addr = \"127.0.0.1:%d\"\n\n", minP)

	for _, sb := range bindings {
		// rathole service names must not contain '/' — flatten with double underscore.
		name := sb.SiteSlug + "__" + sb.ServiceSlug
		fmt.Fprintf(&b, "[server.services.%s]\n", name)
		fmt.Fprintf(&b, "token = %q\n", sb.AgentToken)
		fmt.Fprintf(&b, "bind_addr = \"127.0.0.1:%d\"\n\n", sb.RelayPort)
	}

	return b.String(), nil
}
