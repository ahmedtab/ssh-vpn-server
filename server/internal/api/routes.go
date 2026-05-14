// Package api wires all HTTP routes for the vpn-api control plane.
package api

import (
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"registry.app.circle360.net/ssh-vpn/control-plane/internal/audit"
	"registry.app.circle360.net/ssh-vpn/control-plane/internal/config"
	"registry.app.circle360.net/ssh-vpn/control-plane/internal/logs"
	"registry.app.circle360.net/ssh-vpn/control-plane/internal/middleware"
	"registry.app.circle360.net/ssh-vpn/control-plane/internal/otptoken"
	"registry.app.circle360.net/ssh-vpn/control-plane/internal/pamauth"
	"registry.app.circle360.net/ssh-vpn/control-plane/internal/session"
	"registry.app.circle360.net/ssh-vpn/control-plane/internal/users"
)

// RegisterRoutes attaches all routes to the Gin engine.
func RegisterRoutes(r *gin.Engine, cfg *config.Config, store *session.Store, auditLog *audit.Logger) {
	userMgr := users.NewManager(cfg.UsersDir, cfg.AuthKeysFile)
	logReader := logs.NewReader(cfg.LogsDir)

	otpSecret, _ := hex.DecodeString(cfg.OTPSharedSecret)
	if len(otpSecret) == 0 {
		otpSecret = []byte(cfg.OTPSharedSecret) // fall back to raw bytes if not hex
	}

	// ── Health ──────────────────────────────────────────────────────────────
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// ── Auth pages ───────────────────────────────────────────────────────────
	r.GET("/login", handleLoginPage(store))
	r.POST("/login", handleLoginSubmit(cfg, store, auditLog))
	r.POST("/logout", handleLogout(store))

	// ── Admin web pages (require session) ───────────────────────────────────
	admin := r.Group("/", middleware.RequireSession(store))
	{
		admin.GET("/", handleDashboard(userMgr))
		admin.GET("/users", handleUsersPage(userMgr))
		admin.GET("/logs", handleLogsPage(logReader))
	}

	// ── REST API (require session + CSRF) ────────────────────────────────────
	apiV1 := r.Group("/api/v1", middleware.RequireSession(store))
	{
		// Users CRUD
		apiV1.GET("/users", apiListUsers(userMgr))
		apiV1.POST("/users", middleware.RequireCSRF(store), apiCreateUser(userMgr, auditLog))
		apiV1.GET("/users/:name", apiGetUser(userMgr))
		apiV1.DELETE("/users/:name", middleware.RequireCSRF(store), apiDeleteUser(userMgr, auditLog))

		// Logs
		apiV1.GET("/logs", apiListLogFiles(logReader))
		apiV1.GET("/logs/:file", apiGetLogTail(logReader))
		apiV1.GET("/logs/:file/stream", apiStreamLog(logReader))
	}

	// ── OTP Provisioning — no session required; signed with shared secret ───
	r.POST("/api/v1/provision", apiProvision(cfg, userMgr, auditLog, otpSecret))
}

// ── Auth handlers ─────────────────────────────────────────────────────────────

func handleLoginPage(store *session.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		// If already authenticated, redirect to dashboard.
		if _, err := store.Get(c.Request); err == nil {
			c.Redirect(http.StatusFound, "/")
			return
		}
		c.HTML(http.StatusOK, "login.html", gin.H{
			"Next": c.Query("next"),
		})
	}
}

func handleLoginSubmit(cfg *config.Config, store *session.Store, auditLog *audit.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		username := c.PostForm("username")
		password := c.PostForm("password")
		next := c.PostForm("next")
		if next == "" || !isLocalPath(next) {
			next = "/"
		}

		if err := pamauth.Authenticate(username, password); err != nil {
			slog.Warn("login failed", "username", username, "ip", c.ClientIP())
			auditLog.Log(audit.Event{
				Actor: username, Action: "auth.login", Result: "denied",
				Detail: "pam rejected", RemoteIP: c.ClientIP(),
			})
			c.HTML(http.StatusUnauthorized, "login.html", gin.H{
				"Error": "Invalid username or password.",
				"Next":  next,
			})
			return
		}

		secure := cfg.PublicMode || c.Request.TLS != nil
		_, err := store.Create(c.Writer, username, secure)
		if err != nil {
			c.HTML(http.StatusInternalServerError, "login.html", gin.H{"Error": "Session error. Try again."})
			return
		}
		auditLog.Log(audit.Event{
			Actor: username, Action: "auth.login", Result: "ok", RemoteIP: c.ClientIP(),
		})
		c.Redirect(http.StatusFound, next)
	}
}

func handleLogout(store *session.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		store.Revoke(c.Request, c.Writer)
		c.Redirect(http.StatusFound, "/login")
	}
}

// ── Admin page handlers ────────────────────────────────────────────────────────

func handleDashboard(userMgr *users.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		userList, _ := userMgr.List()
		sess := getSession(c)
		c.HTML(http.StatusOK, "dashboard.html", gin.H{
			"Session":   sess,
			"UserCount": len(userList),
			"CSRF":      sess.CSRF,
		})
	}
}

func handleUsersPage(userMgr *users.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		userList, err := userMgr.List()
		sess := getSession(c)
		c.HTML(http.StatusOK, "users.html", gin.H{
			"Session": sess,
			"Users":   userList,
			"Error":   errStr(err),
			"CSRF":    sess.CSRF,
		})
	}
}

func handleLogsPage(logReader *logs.Reader) gin.HandlerFunc {
	return func(c *gin.Context) {
		files, _ := logReader.AvailableFiles()
		selected := c.Query("file")
		if selected == "" && len(files) > 0 {
			selected = files[0]
		}
		var entries []logs.Entry
		if selected != "" {
			entries, _ = logReader.ReadTail(selected, 200)
		}
		sess := getSession(c)
		c.HTML(http.StatusOK, "logs.html", gin.H{
			"Session":  sess,
			"Files":    files,
			"Selected": selected,
			"Entries":  entries,
			"CSRF":     sess.CSRF,
		})
	}
}

// ── REST API handlers ─────────────────────────────────────────────────────────

func apiListUsers(userMgr *users.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		list, err := userMgr.List()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, list)
	}
}

func apiGetUser(userMgr *users.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, err := userMgr.Get(c.Param("name"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, u)
	}
}

type createUserReq struct {
	Username  string `json:"username"  form:"username"  binding:"required"`
	PublicKey string `json:"public_key" form:"public_key" binding:"required"`
}

func apiCreateUser(userMgr *users.Manager, auditLog *audit.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req createUserReq
		if err := c.ShouldBind(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		u, err := userMgr.Add(req.Username, req.PublicKey)
		sess := getSession(c)
		if err != nil {
			auditLog.Log(audit.Event{
				Actor: sess.Username, Action: "user.create", Target: req.Username,
				Result: "error", Detail: err.Error(), RemoteIP: c.ClientIP(),
			})
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		auditLog.Log(audit.Event{
			Actor: sess.Username, Action: "user.create", Target: req.Username,
			Result: "ok", RemoteIP: c.ClientIP(),
		})
		// If the request came from a browser form, redirect back.
		if c.GetHeader("Accept") != "application/json" {
			c.Redirect(http.StatusFound, "/users")
			return
		}
		c.JSON(http.StatusCreated, u)
	}
}

func apiDeleteUser(userMgr *users.Manager, auditLog *audit.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("name")
		if err := userMgr.Remove(name); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		sess := getSession(c)
		auditLog.Log(audit.Event{
			Actor: sess.Username, Action: "user.delete", Target: name,
			Result: "ok", RemoteIP: c.ClientIP(),
		})
		c.JSON(http.StatusOK, gin.H{"deleted": name})
	}
}

func apiListLogFiles(logReader *logs.Reader) gin.HandlerFunc {
	return func(c *gin.Context) {
		files, err := logReader.AvailableFiles()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, files)
	}
}

func apiGetLogTail(logReader *logs.Reader) gin.HandlerFunc {
	return func(c *gin.Context) {
		n, _ := strconv.Atoi(c.DefaultQuery("n", "100"))
		if n < 1 || n > 5000 {
			n = 100
		}
		entries, err := logReader.ReadTail(c.Param("file"), n)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, entries)
	}
}

func apiStreamLog(logReader *logs.Reader) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no") // disable nginx buffering

		done := c.Request.Context().Done()
		_ = logReader.StreamTo(c.Param("file"), c.Writer, done)
	}
}

// ── Provisioning endpoint (OTP / Windows client) ──────────────────────────────

func apiProvision(cfg *config.Config, userMgr *users.Manager, auditLog *audit.Logger, secret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req otptoken.ProvisionRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := otptoken.Validate(&req, secret, cfg.OTPTokenTTL); err != nil {
			slog.Warn("provision rejected", "username", req.Username, "ip", c.ClientIP(), "reason", err.Error())
			auditLog.Log(audit.Event{
				Actor: req.Username, Action: "provision", Target: req.Username,
				Result: "denied", Detail: err.Error(), RemoteIP: c.ClientIP(),
			})
			// Intentionally vague error to avoid leaking internal details.
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired provisioning request"})
			return
		}

		// Auto-create the user if they don't exist yet.
		var u *users.User
		var err error
		if userMgr.Exists(req.Username) {
			u, err = userMgr.Get(req.Username)
		} else {
			u, err = userMgr.Add(req.Username, req.PublicKey)
		}
		if err != nil {
			slog.Error("provision: user create failed", "username", req.Username, "err", err)
			auditLog.Log(audit.Event{
				Actor: req.Username, Action: "provision", Target: req.Username,
				Result: "error", Detail: err.Error(), RemoteIP: c.ClientIP(),
			})
			c.JSON(http.StatusInternalServerError, gin.H{"error": "user provisioning failed"})
			return
		}

		auditLog.Log(audit.Event{
			Actor: req.Username, Action: "provision", Target: req.Username,
			Result: "ok", RemoteIP: c.ClientIP(),
		})
		c.JSON(http.StatusOK, gin.H{
			"username":  u.Username,
			"tun_num":   u.TunNum,
			"server_ip": u.ServerIP,
			"client_ip": u.ClientIP,
		})
	}
}

// ── Utilities ─────────────────────────────────────────────────────────────────

func getSession(c *gin.Context) *session.Data {
	if s, exists := c.Get("session"); exists {
		return s.(*session.Data)
	}
	return &session.Data{}
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// isLocalPath ensures next= redirect targets are relative paths only,
// preventing open redirect vulnerabilities.
func isLocalPath(p string) bool {
	if len(p) == 0 || p[0] != '/' {
		return false
	}
	if len(p) > 1 && p[1] == '/' {
		return false // //evil.com
	}
	return true
}
