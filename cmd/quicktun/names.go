package main

import (
	"fmt"
	"strings"
)

// canonicalProjectName accepts "p1" or "projects/p1" and returns
// "projects/p1". Pure string manipulation — slug validation belongs to
// the server (resource.ValidateSlug). We just normalize the form so the
// CLI accepts both shapes.
func canonicalProjectName(s string) string {
	if strings.HasPrefix(s, "projects/") {
		return s
	}
	return "projects/" + s
}

// canonicalSiteName accepts "p1/s1" or "projects/p1/sites/s1" and
// returns the 4-segment form. Errors on malformed input so the CLI can
// fail fast before dialing the server.
func canonicalSiteName(s string) (string, error) {
	parts := strings.Split(s, "/")
	switch len(parts) {
	case 2:
		if parts[0] == "" || parts[1] == "" {
			return "", fmt.Errorf("invalid site name %q (expected p/s or projects/p/sites/s)", s)
		}
		return fmt.Sprintf("projects/%s/sites/%s", parts[0], parts[1]), nil
	case 4:
		if parts[0] == "projects" && parts[2] == "sites" && parts[1] != "" && parts[3] != "" {
			return s, nil
		}
	}
	return "", fmt.Errorf("invalid site name %q (expected p/s or projects/p/sites/s)", s)
}

// canonicalServiceName accepts "p/s/svc" or
// "projects/p/sites/s/services/svc" and returns the 6-segment form.
func canonicalServiceName(s string) (string, error) {
	parts := strings.Split(s, "/")
	switch len(parts) {
	case 3:
		if parts[0] == "" || parts[1] == "" || parts[2] == "" {
			return "", fmt.Errorf("invalid service name %q (expected p/s/svc or projects/p/sites/s/services/svc)", s)
		}
		return fmt.Sprintf("projects/%s/sites/%s/services/%s", parts[0], parts[1], parts[2]), nil
	case 6:
		if parts[0] == "projects" && parts[2] == "sites" && parts[4] == "services" &&
			parts[1] != "" && parts[3] != "" && parts[5] != "" {
			return s, nil
		}
	}
	return "", fmt.Errorf("invalid service name %q (expected p/s/svc or projects/p/sites/s/services/svc)", s)
}

// projectFromSiteName extracts "projects/p" from a 4-segment site name.
// Useful when a service create command needs to know which project a
// site belongs to without re-parsing the full path elsewhere.
func projectFromSiteName(siteName string) string {
	parts := strings.Split(siteName, "/")
	if len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return ""
}
