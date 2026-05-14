package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all runtime configuration for the vpn-api server.
type Config struct {
	// Listen address for the HTTP/HTTPS server.
	// Default: :8080 (internal) or :8443 (public TLS)
	Listen string

	// PublicMode enables stricter security controls (HSTS, no local-only bypass).
	// Set VPN_API_PUBLIC=true to enable.
	PublicMode bool

	// TLS certificate and key paths. Leave empty for plain HTTP.
	TLSCert string
	TLSKey  string

	// SessionSecret is used to sign session cookies (min 32 bytes).
	SessionSecret string
	SessionMaxAge time.Duration

	// JWTSecret signs API bearer tokens for the OTP/provisioning endpoint.
	JWTSecret string

	// RateLimit is max requests per second per IP (0 = disabled).
	RateLimit float64

	// AuditLogPath is the file path for structured audit events.
	AuditLogPath string

	// VPN state directories (must match entrypoint.sh / manage-users.sh).
	UsersDir    string
	AuthKeysFile string
	LogsDir     string
	TokensDir   string

	// Provisioning OTP shared secret (HMAC-SHA256 key), base64-encoded.
	// Windows client must hold the matching secret.
	OTPSharedSecret string

	// OTPTokenTTL is how long a provisioning OTP token is valid.
	OTPTokenTTL time.Duration

	// Debug enables verbose logging and Gin debug mode.
	Debug bool
}

// Load reads configuration from environment variables with safe defaults.
func Load() *Config {
	publicMode := getEnvBool("VPN_API_PUBLIC", false)

	listenDefault := ":8080"
	if publicMode {
		listenDefault = ":8443"
	}

	return &Config{
		Listen:          getEnv("VPN_API_LISTEN", listenDefault),
		PublicMode:      publicMode,
		TLSCert:         getEnv("VPN_API_TLS_CERT", ""),
		TLSKey:          getEnv("VPN_API_TLS_KEY", ""),
		SessionSecret:   requireEnv("VPN_API_SESSION_SECRET", "changeme-set-in-production-32chars!"),
		SessionMaxAge:   getEnvDuration("VPN_API_SESSION_MAX_AGE", 8*time.Hour),
		JWTSecret:       requireEnv("VPN_API_JWT_SECRET", "changeme-jwt-secret-32chars!!!!!!"),
		RateLimit:       getEnvFloat("VPN_API_RATE_LIMIT", 10),
		AuditLogPath:    getEnv("VPN_API_AUDIT_LOG", "/var/log/vpn/audit.log"),
		UsersDir:        getEnv("VPN_USERS_DIR", "/etc/vpn/users"),
		AuthKeysFile:    getEnv("VPN_AUTH_KEYS", "/root/.ssh/authorized_keys"),
		LogsDir:         getEnv("VPN_LOGS_DIR", "/var/log/vpn"),
		TokensDir:       getEnv("VPN_TOKENS_DIR", "/etc/vpn/tokens"),
		OTPSharedSecret: requireEnv("VPN_OTP_SHARED_SECRET", "changeme-otp-secret-32chars!!!!!!"),
		OTPTokenTTL:     getEnvDuration("VPN_OTP_TOKEN_TTL", 10*time.Minute),
		Debug:           getEnvBool("VPN_API_DEBUG", false),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// requireEnv returns the env value or the fallback, but logs a warning in production
// if the fallback default is unchanged (detected in main via PublicMode).
func requireEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func getEnvFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
