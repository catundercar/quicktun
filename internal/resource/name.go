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

	minSlugLen = 1
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

const collectionSites = "sites"

// SiteName carries the parsed (project_slug, site_slug) tuple.
type SiteName struct {
	Project string
	Site    string
}

// FormatSiteName returns "projects/{project}/sites/{site}".
func FormatSiteName(projectSlug, siteSlug string) string {
	return collectionProjects + "/" + projectSlug + "/" + collectionSites + "/" + siteSlug
}

// ParseSiteName parses "projects/{project}/sites/{site}" into a SiteName.
func ParseSiteName(name string) (SiteName, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != collectionProjects || parts[2] != collectionSites {
		return SiteName{}, errors.New(`resource: site name must be "projects/{p}/sites/{s}"`)
	}
	if err := ValidateSlug(parts[1]); err != nil {
		return SiteName{}, err
	}
	if err := ValidateSlug(parts[3]); err != nil {
		return SiteName{}, err
	}
	return SiteName{Project: parts[1], Site: parts[3]}, nil
}

// ParseProjectParent parses "projects/{project}" used as a List request parent.
// Same as ParseProjectName; aliased for readability.
func ParseProjectParent(parent string) (string, error) {
	return ParseProjectName(parent)
}

const collectionServices = "services"

// ServiceName carries the parsed (project_slug, site_slug, service_slug) tuple.
type ServiceName struct {
	Project string
	Site    string
	Service string
}

// FormatServiceName returns "projects/{p}/sites/{s}/services/{svc}".
func FormatServiceName(projectSlug, siteSlug, serviceSlug string) string {
	return collectionProjects + "/" + projectSlug + "/" +
		collectionSites + "/" + siteSlug + "/" +
		collectionServices + "/" + serviceSlug
}

// ParseServiceName parses "projects/{p}/sites/{s}/services/{svc}".
func ParseServiceName(name string) (ServiceName, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 6 ||
		parts[0] != collectionProjects ||
		parts[2] != collectionSites ||
		parts[4] != collectionServices {
		return ServiceName{}, errors.New(`resource: service name must be "projects/{p}/sites/{s}/services/{svc}"`)
	}
	if err := ValidateSlug(parts[1]); err != nil {
		return ServiceName{}, err
	}
	if err := ValidateSlug(parts[3]); err != nil {
		return ServiceName{}, err
	}
	if err := ValidateSlug(parts[5]); err != nil {
		return ServiceName{}, err
	}
	return ServiceName{Project: parts[1], Site: parts[3], Service: parts[5]}, nil
}

// FormatSiteParent returns "projects/{p}/sites/{s}" used as a List parent.
func FormatSiteParent(projectSlug, siteSlug string) string {
	return collectionProjects + "/" + projectSlug + "/" + collectionSites + "/" + siteSlug
}

// ParseSiteParent parses "projects/{p}/sites/{s}" used as a List request parent.
func ParseSiteParent(parent string) (SiteName, error) {
	return ParseSiteName(parent)
}
