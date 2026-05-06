package relay_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/model"
	"github.com/tulip/quicktun/internal/relay"
)

func TestRenderRatholeServerEmptyServices(t *testing.T) {
	p := &model.Project{
		Base: model.Base{ID: 1}, Slug: "p1", RelayPortRange: "20000-20099",
	}
	out, err := relay.RenderRatholeServer(p, nil)
	require.NoError(t, err)
	require.Contains(t, out, `[server]`)
	require.Contains(t, out, `bind_addr = "127.0.0.1:20000"`)
	require.NotContains(t, out, `[server.services.`)
}

func TestRenderRatholeServerWithServices(t *testing.T) {
	p := &model.Project{
		Base: model.Base{ID: 1}, Slug: "clinic-network", RelayPortRange: "20000-20099",
	}
	rp22 := uint16(20022)
	rp33 := uint16(20033)
	binding := []relay.ServiceBinding{
		{
			SiteSlug:    "bastion-1",
			ServiceSlug: "ssh",
			RelayPort:   rp22,
			AgentToken:  "site1-token-hash",
		},
		{
			SiteSlug:    "bastion-1",
			ServiceSlug: "rdp",
			RelayPort:   rp33,
			AgentToken:  "site1-token-hash",
		},
	}
	out, err := relay.RenderRatholeServer(p, binding)
	require.NoError(t, err)
	require.Contains(t, out, `bind_addr = "127.0.0.1:20000"`)
	require.Contains(t, out, `[server.services.bastion-1__ssh]`)
	require.Contains(t, out, `bind_addr = "127.0.0.1:20022"`)
	require.Contains(t, out, `[server.services.bastion-1__rdp]`)
	require.Contains(t, out, `bind_addr = "127.0.0.1:20033"`)
	require.Contains(t, out, `token = "site1-token-hash"`)
	require.True(t, strings.HasPrefix(out, "# quicktun-rendered"))
}

func TestRenderRatholeServerRejectsBadRange(t *testing.T) {
	p := &model.Project{
		Base: model.Base{ID: 1}, Slug: "p", RelayPortRange: "garbage",
	}
	_, err := relay.RenderRatholeServer(p, nil)
	require.Error(t, err)
}
