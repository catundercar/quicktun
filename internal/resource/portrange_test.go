package resource_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/resource"
)

func TestParsePortRange(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		minP, maxP, err := resource.ParsePortRange("20000-20099")
		require.NoError(t, err)
		require.Equal(t, uint16(20000), minP)
		require.Equal(t, uint16(20099), maxP)
	})
	t.Run("trims whitespace", func(t *testing.T) {
		_, _, err := resource.ParsePortRange(" 100 - 200 ")
		require.NoError(t, err)
	})
	t.Run("missing dash", func(t *testing.T) {
		_, _, err := resource.ParsePortRange("garbage")
		require.Error(t, err)
	})
	t.Run("non-numeric min", func(t *testing.T) {
		_, _, err := resource.ParsePortRange("abc-100")
		require.Error(t, err)
	})
	t.Run("non-numeric max", func(t *testing.T) {
		_, _, err := resource.ParsePortRange("100-xyz")
		require.Error(t, err)
	})
	t.Run("min greater than max", func(t *testing.T) {
		_, _, err := resource.ParsePortRange("200-100")
		require.Error(t, err)
	})
	t.Run("equal min and max rejected", func(t *testing.T) {
		_, _, err := resource.ParsePortRange("20000-20000")
		require.Error(t, err)
		require.Contains(t, err.Error(), "narrow")
	})
	t.Run("port out of range", func(t *testing.T) {
		_, _, err := resource.ParsePortRange("100-99999")
		require.Error(t, err)
	})
}
