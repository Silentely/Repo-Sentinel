package httpapi

import (
	"bytes"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
)

const (
	spaIndexCacheControl = "no-cache"
	spaAssetCacheControl = "public, max-age=31536000, immutable"
)

type spaHandler struct {
	files    fs.FS
	notFound http.Handler
}

func newSPAHandler(files fs.FS, notFound http.Handler) http.Handler {
	if notFound == nil {
		notFound = http.NotFoundHandler()
	}
	return &spaHandler{files: files, notFound: notFound}
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.files == nil || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		h.notFound.ServeHTTP(w, r)
		return
	}

	name, ok := safeSPAPath(r.URL)
	if !ok || isReservedHTTPPath(name) {
		h.notFound.ServeHTTP(w, r)
		return
	}
	if name == "index.html" {
		if !h.serveFile(w, r, name, spaIndexCacheControl) {
			h.notFound.ServeHTTP(w, r)
		}
		return
	}
	if h.serveFile(w, r, name, spaAssetCacheControl) {
		return
	}
	if name == "assets" || strings.HasPrefix(name, "assets/") || path.Ext(name) != "" {
		h.notFound.ServeHTTP(w, r)
		return
	}
	if !h.serveFile(w, r, "index.html", spaIndexCacheControl) {
		h.notFound.ServeHTTP(w, r)
	}
}

func safeSPAPath(requestURL *url.URL) (string, bool) {
	if requestURL == nil {
		return "", false
	}
	escaped := requestURL.EscapedPath()
	if escaped == "" {
		escaped = "/"
	}
	lowerEscaped := strings.ToLower(escaped)
	for _, unsafeEscape := range []string{"%00", "%25", "%2e", "%2f", "%5c"} {
		if strings.Contains(lowerEscaped, unsafeEscape) {
			return "", false
		}
	}
	decoded, err := url.PathUnescape(escaped)
	if err != nil || !strings.HasPrefix(decoded, "/") || strings.ContainsRune(decoded, '\x00') || strings.Contains(decoded, "\\") {
		return "", false
	}
	for _, segment := range strings.Split(decoded, "/") {
		if segment == "." || segment == ".." {
			return "", false
		}
	}

	cleaned := path.Clean(decoded)
	if cleaned == "/" {
		return "index.html", true
	}
	name := strings.TrimPrefix(cleaned, "/")
	if !fs.ValidPath(name) {
		return "", false
	}
	return name, true
}

func isReservedHTTPPath(name string) bool {
	requestPath := "/" + name
	return requestPath == "/api" || strings.HasPrefix(requestPath, "/api/") ||
		requestPath == "/health" || strings.HasPrefix(requestPath, "/health/")
}

func (h *spaHandler) serveFile(w http.ResponseWriter, r *http.Request, name, cacheControl string) bool {
	file, err := h.files.Open(name)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	contents, err := io.ReadAll(file)
	if err != nil {
		return false
	}

	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		contentType = http.DetectContentType(contents)
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", cacheControl)
	http.ServeContent(w, r, name, info.ModTime(), bytes.NewReader(contents))
	return true
}
