package proxy

import (
	"net/url"
	"strings"
)

// Route defines a proxy route mapping
type Route struct {
	Path         string
	TargetURL    string
	UpstreamPath string
}

// Router manages route mappings
type Router struct {
	routes []Route
}

// NewRouter creates a new router
func NewRouter(auditURL, trackerURL string) *Router {
	return &Router{
		routes: []Route{
			{Path: "/bff/v1/audit", TargetURL: auditURL, UpstreamPath: "/api/v1/audit"},
			{Path: "/bff/v1/analytics", TargetURL: auditURL, UpstreamPath: "/api/v1/analytics"},
			{Path: "/bff/v1/incidents", TargetURL: auditURL, UpstreamPath: "/api/v1/incidents"},
			{Path: "/bff/v1/retention", TargetURL: auditURL, UpstreamPath: "/api/v1/retention"},
			{Path: "/bff/v1/tasktracker", TargetURL: trackerURL, UpstreamPath: ""},
		},
	}
}

// FindRoute finds the target URL for a given path
// Returns the target URL and upstream path, or empty strings if no match.
func (r *Router) FindRoute(path string) (string, string) {
	for _, route := range r.routes {
		if remainingPath, ok := matchRoute(route.Path, path); ok {
			return route.TargetURL, joinPaths(route.UpstreamPath, remainingPath)
		}
	}
	return "", ""
}

func matchRoute(routePath, requestPath string) (string, bool) {
	if requestPath == routePath {
		return "", true
	}
	if strings.HasPrefix(requestPath, routePath+"/") {
		return strings.TrimPrefix(requestPath, routePath), true
	}
	return "", false
}

func joinPaths(basePath, suffix string) string {
	if basePath == "" {
		if suffix == "" {
			return "/"
		}
		return ensureLeadingSlash(suffix)
	}

	basePath = ensureLeadingSlash(strings.TrimRight(basePath, "/"))
	if suffix == "" {
		return basePath
	}

	return basePath + "/" + strings.TrimLeft(suffix, "/")
}

func ensureLeadingSlash(path string) string {
	if path == "" {
		return "/"
	}
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

// BuildUpstreamURL constructs the full upstream URL
func BuildUpstreamURL(targetURL, path string) (string, error) {
	base, err := url.Parse(targetURL)
	if err != nil {
		return "", err
	}

	base.Path = ensureLeadingSlash(path)
	return base.String(), nil
}
