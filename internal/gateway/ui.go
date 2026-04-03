package gateway

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

// Build information injected at build time.
var (
	// BuildVersion is the application version (e.g., "v1.0.0")
	BuildVersion = "dev"
	// BuildTime is the build timestamp (e.g., "2024-01-15T10:00:00Z")
	BuildTime = "unknown"
	// BuildCommit is the git commit hash
	BuildCommit = "unknown"
	// UIAssetsVersion is derived from the static assets so it changes automatically.
	UIAssetsVersion = ""
)

func init() {
	if UIAssetsVersion == "" {
		UIAssetsVersion = computeUIAssetsVersion()
	}
}

//go:embed ui/*
var uiAssets embed.FS

// uiFileSystem is a wrapper around embed.FS that supports both embedded and filesystem mode.
type uiFileSystem struct {
	embedded  embed.FS
	devMode   bool
	devFSRoot string
}

func computeUIAssetsVersion() string {
	files := []string{"app.js", "styles.css"}
	hasher := sha256.New()
	for _, name := range files {
		data, err := uiAssets.ReadFile(path.Join("ui", name))
		if err != nil {
			continue
		}
		hasher.Write(data)
	}
	return hex.EncodeToString(hasher.Sum(nil))[:8]
}

// newUIFileSystem creates a new UI filesystem handler.
// In dev mode, files are read from the filesystem for hot-reload during development.
func newUIFileSystem(devMode bool) *uiFileSystem {
	return &uiFileSystem{
		embedded:  uiAssets,
		devMode:   devMode,
		devFSRoot: "internal/gateway/ui",
	}
}

// ReadFile reads a file from the embedded FS or filesystem.
func (u *uiFileSystem) ReadFile(name string) ([]byte, error) {
	// Sanitize path
	name = strings.TrimPrefix(name, "/")
	name = strings.TrimPrefix(name, "ui/")

	if u.devMode {
		// In dev mode, try to read from filesystem for hot-reload
		fullPath := path.Join(u.devFSRoot, name)
		if data, err := os.ReadFile(fullPath); err == nil {
			return data, nil
		}
		// Fallback to embedded FS if file not found in filesystem
	}

	// In production mode (or dev fallback), read from embedded FS
	return u.embedded.ReadFile(path.Join("ui", name))
}

// Open opens a file from the embedded FS.
func (u *uiFileSystem) Open(name string) (fs.File, error) {
	return u.embedded.Open(name)
}

// uiHandler handles serving the embedded UI assets.
type uiHandler struct {
	fs        *uiFileSystem
	cacheTime time.Duration
}

// newUIHandler creates a new UI handler with caching support.
func newUIHandler(devMode bool) *uiHandler {
	cacheTime := 24 * time.Hour
	if devMode {
		cacheTime = 0 // No cache in dev mode
	}
	return &uiHandler{
		fs:        newUIFileSystem(devMode),
		cacheTime: cacheTime,
	}
}

// ServeHTTP serves UI assets with proper caching headers.
func (h *uiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Handle index.html for root and non-API routes
	if r.URL.Path == "/" || (!strings.HasPrefix(r.URL.Path, "/api/") &&
		!strings.HasPrefix(r.URL.Path, "/ws") &&
		!strings.HasPrefix(r.URL.Path, "/rpc") &&
		!strings.HasPrefix(r.URL.Path, "/assets/")) {
		h.serveIndexHTML(w, r)
		return
	}

	// Handle /assets/* routes
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		h.serveAsset(w, r)
		return
	}

	http.NotFound(w, r)
}

// serveIndexHTML serves the main index.html with build information.
func (h *uiHandler) serveIndexHTML(w http.ResponseWriter, r *http.Request) {
	data, err := h.fs.ReadFile("index.html")
	if err != nil {
		http.Error(w, "UI unavailable", http.StatusInternalServerError)
		return
	}

	// Inject build information and cache-busting version
	html := string(data)

	// Add version query parameter to static assets for cache busting
	assetVersion := UIAssetsVersion

	// Replace asset URLs with versioned URLs
	html = strings.Replace(html, `href="/assets/styles.css"`, fmt.Sprintf(`href="/assets/styles.css?v=%s"`, assetVersion), 1)
	html = strings.Replace(html, `src="/assets/app.js"`, fmt.Sprintf(`src="/assets/app.js?v=%s"`, assetVersion), 1)

	// Inject build information into HTML head
	html = strings.Replace(html, "<head>", fmt.Sprintf(`<head>
<meta name="app-version" content="%s">
<meta name="build-time" content="%s">
<meta name="build-commit" content="%s">`, BuildVersion, BuildTime, BuildCommit), 1)

	h.setCacheHeaders(w, "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
}

// serveAsset serves static assets with proper MIME types and caching.
func (h *uiHandler) serveAsset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/assets/")
	if name == "" {
		http.NotFound(w, r)
		return
	}

	// Security check: prevent directory traversal
	if strings.Contains(name, "..") || strings.Contains(name, "/") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	data, err := h.fs.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Set content type based on file extension
	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	h.setCacheHeaders(w, contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// setCacheHeaders sets caching headers for the response.
func (h *uiHandler) setCacheHeaders(w http.ResponseWriter, contentType string) {
	w.Header().Set("Content-Type", contentType)

	if h.cacheTime > 0 {
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", int(h.cacheTime.Seconds())))
		w.Header().Set("Expires", time.Now().Add(h.cacheTime).UTC().Format(http.TimeFormat))
	} else {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
	}
}

// handleUI handles requests for the main UI page.
func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	s.uiHandler.ServeHTTP(w, r)
}

// handleUIAssets handles requests for UI static assets.
func (s *Server) handleUIAssets(w http.ResponseWriter, r *http.Request) {
	s.uiHandler.ServeHTTP(w, r)
}

// GetBuildInfo returns build information.
func GetBuildInfo() map[string]string {
	return map[string]string{
		"version": BuildVersion,
		"time":    BuildTime,
		"commit":  BuildCommit,
	}
}

// mimeTypes adds additional MIME types not included in the standard library.
func init() {
	// Add common MIME types that might be missing
	_ = mime.AddExtensionType(".js", "application/javascript")
	_ = mime.AddExtensionType(".mjs", "application/javascript")
	_ = mime.AddExtensionType(".css", "text/css; charset=utf-8")
	_ = mime.AddExtensionType(".html", "text/html; charset=utf-8")
	_ = mime.AddExtensionType(".json", "application/json")
	_ = mime.AddExtensionType(".svg", "image/svg+xml")
	_ = mime.AddExtensionType(".woff", "font/woff")
	_ = mime.AddExtensionType(".woff2", "font/woff2")
	_ = mime.AddExtensionType(".ttf", "font/ttf")
	_ = mime.AddExtensionType(".eot", "application/vnd.ms-fontobject")
	_ = mime.AddExtensionType(".ico", "image/x-icon")
	_ = mime.AddExtensionType(".webp", "image/webp")
	_ = mime.AddExtensionType(".png", "image/png")
	_ = mime.AddExtensionType(".jpg", "image/jpeg")
	_ = mime.AddExtensionType(".jpeg", "image/jpeg")
	_ = mime.AddExtensionType(".gif", "image/gif")
}
