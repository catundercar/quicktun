// Package resource parses and formats Google AIP-122 resource names.
//
// Resource names look like "projects/{project}", "operators/{operator}", etc.
// Slugs are URL-safe lowercase strings: [a-z0-9][a-z0-9-]*[a-z0-9], length
// 3-64. Tokens like "Project" or paths with extra segments are rejected.
package resource

import (
	"errors"
	"fmt"
	"strings"
)

const (
	collectionProjects = "projects"

	minSlugLen = 3
	maxSlugLen = 64
)

// ValidateSlug returns an error if s is not a valid resource ID slug.
//
// Valid: 3-64 chars, [a-z0-9][a-z0-9-]*[a-z0-9], no consecutive dashes.
func ValidateSlug(s string) error {
	if len(s) < minSlugLen || len(s) > maxSlugLen {
		return fmt.Errorf("resource: slug %q must be %d-%d chars", s, minSlugLen, maxSlugLen)
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
			if i == 0 || i == len(s)-1 {
				return fmt.Errorf("resource: slug %q must not start or end with '-'", s)
			}
			if s[i-1] == '-' {
				return fmt.Errorf("resource: slug %q must not contain '--'", s)
			}
		default:
			return fmt.Errorf("resource: slug %q contains invalid char %q", s, r)
		}
	}
	return nil
}

// FormatProjectName returns the resource name for a project slug.
func FormatProjectName(slug string) string {
	return collectionProjects + "/" + slug
}

// ParseProjectName parses "projects/{slug}" and returns the slug.
func ParseProjectName(name string) (string, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 2 || parts[0] != collectionProjects {
		return "", errors.New(`resource: project name must be "projects/{slug}"`)
	}
	if err := ValidateSlug(parts[1]); err != nil {
		return "", err
	}
	return parts[1], nil
}
