// Package model holds the store connection types shared by the API,
// the PostGIS/S3 clients and the mapfile generator.
package model

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

const (
	TypePostGIS = "postgis"
	TypeS3      = "s3"
	TypeLocal   = "local"
)

// Names are used as pg_service sections, mapfile identifiers and URL parts.
var nameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,62}$`)

func ValidName(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("name %q must start with a letter and contain only letters, digits and _ (max 63)", name)
	}
	return nil
}

// PostGISConfig is the stored (non-secret) part of a PostGIS store.
type PostGISConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	User     string `json:"user"`
	Schema   string `json:"schema"`
	SSLMode  string `json:"sslmode,omitempty"`
}

// PostGISConn is a full connection: config plus decrypted password.
type PostGISConn struct {
	PostGISConfig
	Password string
}

func (c *PostGISConfig) Normalize() []string {
	var errs []string
	c.Host = strings.TrimSpace(c.Host)
	if c.Port == 0 {
		c.Port = 5432
	}
	if c.Schema == "" {
		c.Schema = "public"
	}
	if c.SSLMode == "" {
		c.SSLMode = "prefer"
	}
	if c.Host == "" {
		errs = append(errs, "connection.host is required")
	}
	if c.Database == "" {
		errs = append(errs, "connection.database is required")
	}
	if c.User == "" {
		errs = append(errs, "connection.user is required")
	}
	if c.Port < 1 || c.Port > 65535 {
		errs = append(errs, "connection.port must be 1-65535")
	}
	switch c.SSLMode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		errs = append(errs, "connection.sslmode is not a valid libpq sslmode")
	}
	// Values go into pg_service.conf, one per line.
	for _, v := range []string{c.Host, c.Database, c.User, c.Schema} {
		if strings.ContainsAny(v, "\r\n") {
			errs = append(errs, "connection values must not contain line breaks")
			break
		}
	}
	return errs
}

// S3Config is the stored (non-secret) part of an S3 store.
type S3Config struct {
	Bucket         string `json:"bucket"`
	Region         string `json:"region"`
	Endpoint       string `json:"endpoint,omitempty"` // host[:port], empty = AWS
	Prefix         string `json:"prefix,omitempty"`
	AccessKey      string `json:"access_key"`
	UseHTTPS       bool   `json:"use_https"`
	VirtualHosting bool   `json:"virtual_hosting"`
}

// S3Conn is a full connection: config plus decrypted secret key.
type S3Conn struct {
	S3Config
	SecretKey string
}

func (c *S3Config) Normalize() []string {
	var errs []string
	c.Endpoint = strings.TrimSpace(c.Endpoint)
	c.Endpoint = strings.TrimPrefix(strings.TrimPrefix(c.Endpoint, "https://"), "http://")
	c.Endpoint = strings.TrimSuffix(c.Endpoint, "/")
	c.Prefix = strings.TrimPrefix(c.Prefix, "/")
	if c.Prefix != "" && !strings.HasSuffix(c.Prefix, "/") {
		c.Prefix += "/"
	}
	if c.Region == "" {
		c.Region = "us-east-1"
	}
	if c.Bucket == "" {
		errs = append(errs, "connection.bucket is required")
	}
	if c.AccessKey == "" {
		errs = append(errs, "connection.access_key is required")
	}
	for _, v := range []string{c.Bucket, c.Region, c.Endpoint, c.Prefix, c.AccessKey} {
		if strings.ContainsAny(v, "\r\n\"") {
			errs = append(errs, "connection values must not contain line breaks or quotes")
			break
		}
	}
	return errs
}

// EndpointOrAWS returns the endpoint to talk to.
func (c S3Config) EndpointOrAWS() string {
	if c.Endpoint != "" {
		return c.Endpoint
	}
	return "s3." + c.Region + ".amazonaws.com"
}

// LocalConfig is a local raster folder store: a folder inside the raster
// directory (MSAPI_RASTER_DIR) that is mounted read-only into both msapi and
// MapServer at the same path. Path is relative to that directory; "" is the
// directory itself. No secret.
type LocalConfig struct {
	Path string `json:"path"`
}

// LocalConn is a local store with its absolute folder resolved.
type LocalConn struct {
	LocalConfig
	Root string // MSAPI_RASTER_DIR
	Dir  string // Root joined with Path
}

func (c *LocalConfig) Normalize() []string {
	p := strings.TrimSpace(strings.ReplaceAll(c.Path, `\`, "/"))
	if strings.ContainsAny(p, "\r\n\"'") {
		return []string{"connection.path must not contain line breaks or quotes"}
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return []string{"connection.path must stay inside the raster directory"}
		}
	}
	c.Path = strings.Trim(path.Clean("/"+p), "/")
	return nil
}
