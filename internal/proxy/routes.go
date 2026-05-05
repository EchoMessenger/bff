package proxy

import (
	"net/url"
	"strings"
)

// Route defines a proxy route mapping
type Route struct {
	Path      string
	TargetURL string
}

// Router manages route mappings
type Router struct {
	routes []Route
}

// NewRouter creates a new router
func NewRouter(auditURL, trackerURL string) *Router {
	return &Router{
		routes: []Route{
			{Path: "/bff/v1/audit/", TargetURL: auditURL},
			{Path: "/bff/v1/tasktracker/", TargetURL: trackerURL},
		},
	}
}

// FindRoute finds the target URL for a given path
// Returns the target URL and remaining path, or empty string if no match
func (r *Router) FindRoute(path string) (string, string) {
	for _, route := range r.routes {
		if strings.HasPrefix(path, route.Path) {
			// Extract remaining path after the route prefix
			remainingPath := strings.TrimPrefix(path, route.Path)
			return route.TargetURL, remainingPath
		}
	}
	return "", ""
}

// BuildUpstreamURL constructs the full upstream URL
func BuildUpstreamURL(targetURL, path string) (string, error) {
	base, err := url.Parse(targetURL)
	if err != nil {
		return "", err
	}

	// Ensure path starts with /
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	base.Path = path
	return base.String(), nil
}
