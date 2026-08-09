package httpapi

import "net/http"

// securityHeadersMiddleware 下发基础安全响应头。
// HSTS 仅在 HTTPS 部署（PublicBaseURL 为 https）时下发：明文部署下发会令浏览器
// 拒绝后续访问，反而破坏可用性；upgrade-insecure-requests 同理按部署方式裁剪。
func (s *server) securityHeadersMiddleware(next http.Handler) http.Handler {
	secure := usesSecureCookies(s.dependencies.Config.HTTP.PublicBaseURL)
	csp := "default-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; img-src 'self' data:; style-src 'self'; form-action 'self'"
	if secure {
		csp += "; upgrade-insecure-requests"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", csp)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set(
			"Permissions-Policy",
			"accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=()",
		)
		if secure {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}
