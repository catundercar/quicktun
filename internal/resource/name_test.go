package resource_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/resource"
)

func TestParseProjectName(t *testing.T) {
	slug, err := resource.ParseProjectName("projects/clinic-network")
	require.NoError(t, err)
	require.Equal(t, "clinic-network", slug)
}

func TestParseProjectNameRejectsBadFormat(t *testing.T) {
	cases := []string{
		"",
		"clinic-network",
		"projects/",
		"projects/clinic/extra",
		"sites/clinic-network",
		"projects/Bad Slug",
	}
	for _, name := range cases {
		_, err := resource.ParseProjectName(name)
		require.Error(t, err, "expected error for %q", name)
	}
}

func TestFormatProjectName(t *testing.T) {
	require.Equal(t, "projects/clinic-network", resource.FormatProjectName("clinic-network"))
}

func TestValidateSlug(t *testing.T) {
	valid := []string{"abc", "abc-def", "a1b", "abc-def-ghi", "x-y-z-9"}
	for _, s := range valid {
		require.NoError(t, resource.ValidateSlug(s), "expected %q to be valid", s)
	}
	invalid := []string{"", "ab", "Abc", "abc_def", "abc def", "abc-", "-abc", "a--b", strings.Repeat("a", 65)}
	for _, s := range invalid {
		require.Error(t, resource.ValidateSlug(s), "expected %q to be invalid", s)
	}
}
