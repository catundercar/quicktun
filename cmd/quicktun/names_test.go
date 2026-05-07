package main

import "testing"

func TestCanonicalProjectName(t *testing.T) {
	cases := map[string]string{
		"p1":              "projects/p1",
		"projects/p1":     "projects/p1",
		"projects/foo-99": "projects/foo-99",
	}
	for in, want := range cases {
		if got := canonicalProjectName(in); got != want {
			t.Errorf("canonicalProjectName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCanonicalSiteName(t *testing.T) {
	okCases := map[string]string{
		"p1/s1":                   "projects/p1/sites/s1",
		"projects/p1/sites/s1":    "projects/p1/sites/s1",
		"projects/foo/sites/bar":  "projects/foo/sites/bar",
	}
	for in, want := range okCases {
		got, err := canonicalSiteName(in)
		if err != nil {
			t.Errorf("canonicalSiteName(%q) returned error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("canonicalSiteName(%q) = %q, want %q", in, got, want)
		}
	}
	badCases := []string{
		"",
		"single",
		"p1/",
		"/s1",
		"projects/p1/wrong/s1",
		"projects/p1/sites/s1/extra",
	}
	for _, in := range badCases {
		if _, err := canonicalSiteName(in); err == nil {
			t.Errorf("canonicalSiteName(%q) expected error, got nil", in)
		}
	}
}

func TestCanonicalServiceName(t *testing.T) {
	okCases := map[string]string{
		"p1/s1/svc1":                            "projects/p1/sites/s1/services/svc1",
		"projects/p1/sites/s1/services/svc1":    "projects/p1/sites/s1/services/svc1",
	}
	for in, want := range okCases {
		got, err := canonicalServiceName(in)
		if err != nil {
			t.Errorf("canonicalServiceName(%q) returned error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("canonicalServiceName(%q) = %q, want %q", in, got, want)
		}
	}
	badCases := []string{
		"",
		"p1/s1",
		"p1/s1/",
		"projects/p1/sites/s1/services",
		"projects/p1/wrong/s1/services/svc",
		"projects/p1/sites/s1/wrong/svc",
	}
	for _, in := range badCases {
		if _, err := canonicalServiceName(in); err == nil {
			t.Errorf("canonicalServiceName(%q) expected error, got nil", in)
		}
	}
}

func TestProjectFromSiteName(t *testing.T) {
	if got := projectFromSiteName("projects/p1/sites/s1"); got != "projects/p1" {
		t.Errorf("projectFromSiteName(...) = %q, want projects/p1", got)
	}
	if got := projectFromSiteName(""); got != "" {
		t.Errorf("projectFromSiteName(empty) = %q, want empty", got)
	}
}
