// Package middleware provides Gin middleware for the vpn-api server.
package middleware

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
	"registry.app.circle360.net/ssh-vpn/control-plane/internal/session"
)

// RequestLogger logs each request as a structured JSON event.
func RequestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Info("request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"ip", c.ClientIP(),
			"latency_ms", time.Since(start).Milliseconds(),
		)
	}
}

// perIPLimiters is a simple in-memory per-IP rate limiter map.
// For production at scale use a Redis-backed store; for a VPN control plane
// the number of concurrent admin IPs is tiny so in-memory is fine.
var perIPLimiters = newIPLimiterMap()

// RateLimit creates a per-IP token-bucket rate limiter at `rps` requests/sec.
// Set rps=0 to disable.
func RateLimit(rps float64) gin.HandlerFunc {
	if rps <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	burst := int(rps * 3)
	if burst < 1 {
		burst = 1
	}
	return func(c *gin.Context) {
		lim := perIPLimiters.get(c.ClientIP(), rps, burst)
		if !lim.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}
		c.Next()
	}
}

// SecurityHeaders sets safe HTTP response headers.
// In public mode it also sets HSTS.
func SecurityHeaders(publicMode bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:")
		if publicMode {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
	}
}

// RequireSession is a Gin middleware that validates the session cookie.
// On failure it redirects to the login page (for browser requests) or
// returns 401 (for API requests).
func RequireSession(store *session.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		sess, err := store.Get(c.Request)
		if err != nil {
			if isAPIRequest(c) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
				return
			}
			c.Redirect(http.StatusFound, "/login?next="+c.Request.URL.Path)
			c.Abort()
			return
		}
		// Store session data in context for downstream handlers.
		c.Set("session", sess)
		c.Next()
	}
}

// RequireCSRF validates the CSRF token for state-changing requests.
func RequireCSRF(store *session.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		sess, exists := c.Get("session")
		if !exists {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "missing session"})
			return
		}
		sd := sess.(*session.Data)
		if !store.ValidateCSRF(c.Request, sd) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "invalid CSRF token"})
			return
		}
		c.Next()
	}
}

func isAPIRequest(c *gin.Context) bool {
	p := c.Request.URL.Path
	return len(p) >= 4 && p[:4] == "/api"
}

// ── per-IP limiter map ────────────────────────────────────────────────────────

type ipLimiterMap struct {
	mu sync.Mutex
	m  map[string]*rate.Limiter
}

func newIPLimiterMap() *ipLimiterMap {
	return &ipLimiterMap{m: make(map[string]*rate.Limiter)}
}

func (l *ipLimiterMap) get(ip string, rps float64, burst int) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	if lim, ok := l.m[ip]; ok {
		return lim
	}
	lim := rate.NewLimiter(rate.Limit(rps), burst)
	l.m[ip] = lim
	return lim
}
