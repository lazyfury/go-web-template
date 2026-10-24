package web

import "github.com/gin-gonic/gin"

// SecurityHeaders sets the baseline response headers that stop a browser from
// being talked into sniffing, framing or leaking referrers on an API response.
//
// hsts adds Strict-Transport-Security; it is only meaningful when the
// deployment actually terminates TLS in front of the app (production), and
// browsers ignore it over plain http, so it stays off in development to keep
// local responses honest.
func SecurityHeaders(hsts bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		if hsts {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
	}
}

// NoStore marks responses as uncacheable. API payloads carry access tokens and
// per-user data, and the login response used to be cacheable (no Cache-Control
// at all), so a shared proxy or the browser back/forward cache could keep them.
func NoStore() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("Pragma", "no-cache")
		c.Next()
	}
}
