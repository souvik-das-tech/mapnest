package config

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

const (
	goodKey      = "0123456789abcdef0123456789abcdef"
	goodPassword = "correct-horse-battery"
)

func setValid(t *testing.T) {
	t.Helper()
	t.Setenv("MSAPI_API_KEY", goodKey)
	t.Setenv("MSAPI_ADMIN_PASSWORD", goodPassword)
	t.Setenv("MSAPI_SECRET_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("MSAPI_INSECURE_DEV", "")
}

func TestLoad(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		setValid(t)
		t.Setenv("MSAPI_MAPSERVER_URL", "http://mapserver")
		c, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if c.MapServerURL != "http://mapserver/" {
			t.Errorf("MapServerURL = %q", c.MapServerURL)
		}
		if len(c.SecretKey) != 32 {
			t.Errorf("SecretKey len = %d", len(c.SecretKey))
		}
		if c.SessionTTL != 8*time.Hour || c.SessionIdle != 30*time.Minute {
			t.Errorf("session defaults = %v / %v", c.SessionTTL, c.SessionIdle)
		}
		if c.PublicDocs {
			t.Error("docs should be private by default")
		}
		if len(c.TrustedProxies) == 0 {
			t.Error("default trusted proxies missing")
		}
	})

	t.Run("missing secrets reported together", func(t *testing.T) {
		t.Setenv("MSAPI_API_KEY", "")
		t.Setenv("MSAPI_SECRET_KEY", "")
		t.Setenv("MSAPI_ADMIN_PASSWORD", "")
		_, err := Load()
		for _, want := range []string{"MSAPI_API_KEY", "MSAPI_SECRET_KEY", "MSAPI_ADMIN_PASSWORD"} {
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("err = %v, want mention of %s", err, want)
			}
		}
	})
}

func TestWeakSecretsRefused(t *testing.T) {
	cases := map[string][2]string{
		"old default password": {"MSAPI_ADMIN_PASSWORD", "mapserver"},
		"short password":       {"MSAPI_ADMIN_PASSWORD", "short-pw"},
		"password = username":  {"MSAPI_ADMIN_PASSWORD", "ADMIN"},
		"short api key":        {"MSAPI_API_KEY", "0123456789abcdef"},
		"placeholder api key":  {"MSAPI_API_KEY", "change-me-long-random-string"},
	}
	for name, kv := range cases {
		t.Run(name, func(t *testing.T) {
			setValid(t)
			t.Setenv(kv[0], kv[1])
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), kv[0]) {
				t.Fatalf("err = %v, want %s refused", err, kv[0])
			}
		})
	}

	t.Run("insecure dev allows weak values", func(t *testing.T) {
		setValid(t)
		t.Setenv("MSAPI_ADMIN_PASSWORD", "mapserver")
		t.Setenv("MSAPI_API_KEY", "dev")
		t.Setenv("MSAPI_INSECURE_DEV", "true")
		if _, err := Load(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestParsePrefixes(t *testing.T) {
	got, err := ParsePrefixes(" 10.0.0.0/8, 192.168.1.7 ,none")
	if err != nil || len(got) != 2 || got[1].String() != "192.168.1.7/32" {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := ParsePrefixes("not-an-ip"); err == nil {
		t.Fatal("want error")
	}
	if got, _ := ParsePrefixes("none"); len(got) != 0 {
		t.Fatalf("none = %v", got)
	}
}
