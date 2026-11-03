// Package config reads msapi settings from environment variables.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Listen string // MSAPI_LISTEN, default ":8081"
	APIKey string // MSAPI_API_KEY, required, at least 32 characters

	// Admin UI sign-in. The password has no default and must be strong
	// unless MSAPI_INSECURE_DEV=true (see SECURITY.md).
	AdminUser     string        // MSAPI_ADMIN_USER, default "admin"
	AdminPassword string        // MSAPI_ADMIN_PASSWORD, required, at least 12 characters
	SessionTTL    time.Duration // MSAPI_SESSION_HOURS, default 8: absolute session lifetime
	SessionIdle   time.Duration // MSAPI_SESSION_IDLE_MINUTES, default 30: sign out after inactivity

	// Brute-force protection for POST /auth/login.
	LoginMaxAttempts int           // MSAPI_LOGIN_MAX_ATTEMPTS, default 5 failures per username or client IP
	LoginLockout     time.Duration // MSAPI_LOGIN_LOCKOUT_MINUTES, default 15

	// TrustedProxies may set X-Real-IP / X-Forwarded-For, which give the
	// client IP for logs and rate limits. MSAPI_TRUSTED_PROXIES:
	// comma-separated CIDRs or "none"; default loopback and private ranges
	// (the web UI's nginx on the gis-net Docker network).
	TrustedProxies []netip.Prefix

	// PublicDocs serves /docs and /swagger without sign-in.
	// MSAPI_PUBLIC_DOCS: on|off, default off.
	PublicDocs bool

	// InsecureDev relaxes the password and API key strength checks, for
	// local development only. MSAPI_INSECURE_DEV: true|false, default false.
	InsecureDev bool

	SecretKey    []byte     // MSAPI_SECRET_KEY, required, base64 of 32 bytes (AES-256-GCM)
	DataDir      string     // MSAPI_DATA_DIR, holds msadmin.db
	MapfileDir   string     // MSAPI_MAPFILE_DIR, shared with MapServer
	MapServerURL string     // MSAPI_MAPSERVER_URL, internal URL used for health/preview
	PublicURL    string     // MSAPI_PUBLIC_URL, public MapServer URL for capabilities
	RasterDir    string     // MSAPI_RASTER_DIR, local raster folders (read-only, same path in MapServer)
	RasterCache  string     // MSAPI_RASTER_CACHE_DIR, Cloud Optimized copies of non-COG rasters (same path in MapServer)
	LogLevel     slog.Level // MSAPI_LOG_LEVEL: debug|info|warn|error

	// Validate generated mapfiles with mapserv before going live.
	Validate       bool   // MSAPI_VALIDATE: on|off, default on
	ValidateConfig string // MSAPI_VALIDATE_CONFIG: MapServer config used by the validator

	FontDir string // MSAPI_FONT_DIR: DejaVu TTFs copied into the mapfile dir for labels

	// PROJ's database, for suggesting a CRS for data without SRID.
	ProjDB string // MSAPI_PROJ_DB, default /usr/share/proj/proj.db

	// MSAPI_MAPSERVER_GID: group of MapServer's worker (www-data = 33 in the
	// camptocamp image); pg_service.conf is made readable for it. -1 = don't chown.
	MapServerGID int
}

// DBPath is the SQLite config database file.
func (c Config) DBPath() string { return filepath.Join(c.DataDir, "msadmin.db") }

// defaultTrustedProxies: loopback and private ranges. The API port is bound
// to 127.0.0.1 by default (docker-compose.yml), so only the web UI's nginx
// and local scripts reach it.
const defaultTrustedProxies = "127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7"

// Load reads the environment. All problems are reported together.
func Load() (Config, error) {
	c := Config{
		Listen:        env("MSAPI_LISTEN", ":8081"),
		APIKey:        os.Getenv("MSAPI_API_KEY"),
		AdminUser:     env("MSAPI_ADMIN_USER", "admin"),
		AdminPassword: os.Getenv("MSAPI_ADMIN_PASSWORD"),
		DataDir:       env("MSAPI_DATA_DIR", "./data"),
		MapfileDir:    env("MSAPI_MAPFILE_DIR", "./mapfiles"),
		MapServerURL:  env("MSAPI_MAPSERVER_URL", "http://localhost:8080/"),
		PublicURL:     env("MSAPI_PUBLIC_URL", "http://localhost:8080/"),
		RasterDir:     env("MSAPI_RASTER_DIR", "/rasters"),
		RasterCache:   env("MSAPI_RASTER_CACHE_DIR", "/rastercache"),

		ValidateConfig: env("MSAPI_VALIDATE_CONFIG", "/etc/msapi/mapserver-validate.conf"),
		FontDir:        env("MSAPI_FONT_DIR", "/usr/share/fonts/truetype/dejavu"),
		ProjDB:         env("MSAPI_PROJ_DB", "/usr/share/proj/proj.db"),
	}

	var errs []error
	if gid, err := strconv.Atoi(env("MSAPI_MAPSERVER_GID", "33")); err == nil {
		c.MapServerGID = gid
	} else {
		errs = append(errs, fmt.Errorf("MSAPI_MAPSERVER_GID must be a number"))
	}

	c.InsecureDev = boolEnv("MSAPI_INSECURE_DEV", false, &errs)
	c.PublicDocs = boolEnv("MSAPI_PUBLIC_DOCS", false, &errs)
	c.Validate = boolEnv("MSAPI_VALIDATE", true, &errs)

	c.SessionTTL = time.Duration(intEnv("MSAPI_SESSION_HOURS", 8, 1, 24*30, &errs)) * time.Hour
	c.SessionIdle = time.Duration(intEnv("MSAPI_SESSION_IDLE_MINUTES", 30, 1, 24*60, &errs)) * time.Minute
	c.LoginMaxAttempts = intEnv("MSAPI_LOGIN_MAX_ATTEMPTS", 5, 1, 100, &errs)
	c.LoginLockout = time.Duration(intEnv("MSAPI_LOGIN_LOCKOUT_MINUTES", 15, 1, 24*60, &errs)) * time.Minute

	proxies, err := ParsePrefixes(env("MSAPI_TRUSTED_PROXIES", defaultTrustedProxies))
	if err != nil {
		errs = append(errs, fmt.Errorf("MSAPI_TRUSTED_PROXIES: %w", err))
	}
	c.TrustedProxies = proxies

	errs = append(errs, c.checkSecrets()...)

	key, err := base64.StdEncoding.DecodeString(os.Getenv("MSAPI_SECRET_KEY"))
	switch {
	case err != nil:
		errs = append(errs, fmt.Errorf("MSAPI_SECRET_KEY is not valid base64: %w", err))
	case len(key) != 32:
		errs = append(errs, errors.New("MSAPI_SECRET_KEY must be 32 bytes, base64 encoded (openssl rand -base64 32)"))
	default:
		c.SecretKey = key
	}

	if err := c.LogLevel.UnmarshalText([]byte(env("MSAPI_LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("MSAPI_LOG_LEVEL: %w", err))
	}

	if !strings.HasSuffix(c.MapServerURL, "/") {
		c.MapServerURL += "/"
	}
	return c, errors.Join(errs...)
}

// ParsePrefixes reads comma-separated CIDRs or bare IPs; "none" or an
// empty list trusts nobody.
func ParsePrefixes(list string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, p := range strings.Split(list, ",") {
		p = strings.TrimSpace(p)
		if p == "" || strings.EqualFold(p, "none") {
			continue
		}
		if !strings.Contains(p, "/") {
			a, err := netip.ParseAddr(p)
			if err != nil {
				return out, fmt.Errorf("%q is not an IP or CIDR", p)
			}
			out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
			continue
		}
		pfx, err := netip.ParsePrefix(p)
		if err != nil {
			return out, fmt.Errorf("%q is not an IP or CIDR", p)
		}
		out = append(out, pfx.Masked())
	}
	return out, nil
}

// weakSecrets are placeholders from docs, examples and older releases;
// they are refused unless MSAPI_INSECURE_DEV is on.
var weakSecrets = map[string]bool{
	"mapserver": true, "admin": true, "password": true, "changeme": true, "change-me": true,
	"change-me-long-random-string": true, "admin123": true, "password123": true,
	"mapnest": true, "mapnest-admin": true, "change-me-to-a-long-password": true,
}

// checkSecrets enforces the minimum strength of the admin password and the
// API key. With InsecureDev only their presence is required.
func (c Config) checkSecrets() []error {
	var errs []error
	if c.AdminPassword == "" {
		errs = append(errs, errors.New("MSAPI_ADMIN_PASSWORD must be set (at least 12 characters)"))
	}
	if c.APIKey == "" {
		errs = append(errs, errors.New("MSAPI_API_KEY must be set (at least 32 characters: openssl rand -hex 32)"))
	}
	if c.InsecureDev {
		return errs
	}
	if c.AdminPassword != "" {
		lower := strings.ToLower(c.AdminPassword)
		switch {
		case len(c.AdminPassword) < 12:
			errs = append(errs, errors.New("MSAPI_ADMIN_PASSWORD must be at least 12 characters"))
		case weakSecrets[lower] || lower == strings.ToLower(c.AdminUser):
			errs = append(errs, errors.New("MSAPI_ADMIN_PASSWORD is a default or guessable value; choose another"))
		}
	}
	if c.APIKey != "" {
		switch {
		case len(c.APIKey) < 32:
			errs = append(errs, errors.New("MSAPI_API_KEY must be at least 32 characters (openssl rand -hex 32)"))
		case weakSecrets[strings.ToLower(c.APIKey)] || c.APIKey == c.AdminPassword:
			errs = append(errs, errors.New("MSAPI_API_KEY is a placeholder or equals the admin password (openssl rand -hex 32)"))
		}
	}
	return errs
}

func boolEnv(name string, def bool, errs *[]error) bool {
	switch v := strings.ToLower(strings.TrimSpace(os.Getenv(name))); v {
	case "":
		return def
	case "on", "true", "1", "yes":
		return true
	case "off", "false", "0", "no":
		return false
	default:
		*errs = append(*errs, fmt.Errorf("%s must be on or off, got %q", name, v))
		return def
	}
}

func intEnv(name string, def, min, max int, errs *[]error) int {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min || n > max {
		*errs = append(*errs, fmt.Errorf("%s must be a whole number from %d to %d", name, min, max))
		return def
	}
	return n
}

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}
