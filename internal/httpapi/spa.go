package httpapi

import (
	"bytes"
	"compress/gzip"
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
	// API 与机器端点一律不落入 SPA fallback：GET /mcp、/.well-known/xxx 等未注册路径
	// 若返回 index.html 200，客户端探测会误判服务正常。
	return requestPath == "/api" || strings.HasPrefix(requestPath, "/api/") ||
		requestPath == "/health" || strings.HasPrefix(requestPath, "/health/") ||
		requestPath == "/webhooks" || strings.HasPrefix(requestPath, "/webhooks/") ||
		requestPath == "/metrics" ||
		requestPath == "/mcp" || strings.HasPrefix(requestPath, "/mcp/") ||
		requestPath == "/oauth" || strings.HasPrefix(requestPath, "/oauth/") ||
		strings.HasPrefix(requestPath, "/.well-known/") ||
		requestPath == "/openapi.json" ||
		requestPath == "/auth.md" ||
		requestPath == "/robots.txt" ||
		requestPath == "/sitemap.xml"
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
	// 文本类资源按客户端能力 gzip 压缩，降低自托管出站带宽；带 Range 的请求不压缩
	// （gzip 与字节区间语义冲突），非压缩变体仍可被标准缓存按 Vary 区分。
	if acceptsGzip(r.Header.Get("Accept-Encoding")) && r.Header.Get("Range") == "" && compressibleType(contentType) {
		gz := gzip.NewWriter(w)
		defer gz.Close()
		gzw := &gzipResponseWriter{ResponseWriter: w, gz: gz}
		http.ServeContent(gzw, r, name, info.ModTime(), bytes.NewReader(contents))
		return true
	}
	http.ServeContent(w, r, name, info.ModTime(), bytes.NewReader(contents))
	return true
}

// gzipResponseWriter 包装 http.ResponseWriter，在写出时移除 Content-Length
// 并标记 Content-Encoding / Vary，避免压缩后长度与原始字节数不一致。
type gzipResponseWriter struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	// 304 / HEAD 等无正文响应不需要 Content-Encoding。
	if status != http.StatusNotModified {
		w.Header().Del("Content-Length")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	return w.gz.Write(b)
}

// acceptsGzip 判断 Accept-Encoding 是否显式接受 gzip；
// gzip;q=0 表示不接受，按段解析避免误判。
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "gzip") {
			continue
		}
		return !strings.Contains(part, ";q=0")
	}
	return false
}

// compressibleType 判断内容类型是否值得 gzip（文本类/脚本/JSON/SVG）。
func compressibleType(contentType string) bool {
	if contentType == "" {
		return false
	}
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	switch mt {
	case "application/javascript", "application/json", "application/manifest+json", "image/svg+xml":
		return true
	}
	return strings.HasPrefix(mt, "text/")
}
