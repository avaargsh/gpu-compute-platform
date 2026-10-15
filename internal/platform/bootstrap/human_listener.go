package bootstrap

import (
	"errors"
	"net/url"
	"os"
	"strings"
)

// HumanListener defines an isolated loopback HTTPS read API. Secrets are read
// from a mounted file and never stored in exported configuration or logs.
type HumanListener struct {
	Enabled          bool
	Endpoint         string
	Issuer           string
	Audience         string
	ClientID         string
	ClientSecretFile string
	ServerCert       string
	ServerKey        string
}

func LoadHumanListener(getenv func(string) string, profile string) (HumanListener, error) {
	cfg := HumanListener{
		Endpoint:         strings.TrimSpace(getenv("CONTROL_PLANE_OAUTH_INTROSPECTION_URL")),
		Issuer:           strings.TrimSpace(getenv("CONTROL_PLANE_OAUTH_ISSUER")),
		Audience:         strings.TrimSpace(getenv("CONTROL_PLANE_OAUTH_AUDIENCE")),
		ClientID:         strings.TrimSpace(getenv("CONTROL_PLANE_OAUTH_CLIENT_ID")),
		ClientSecretFile: strings.TrimSpace(getenv("CONTROL_PLANE_OAUTH_CLIENT_SECRET_FILE")),
		ServerCert:       strings.TrimSpace(getenv("CONTROL_PLANE_HUMAN_TLS_CERT")),
		ServerKey:        strings.TrimSpace(getenv("CONTROL_PLANE_HUMAN_TLS_KEY")),
	}
	if cfg.Endpoint == "" && cfg.Issuer == "" && cfg.Audience == "" &&
		cfg.ClientID == "" && cfg.ClientSecretFile == "" &&
		cfg.ServerCert == "" && cfg.ServerKey == "" {
		return HumanListener{}, nil
	}
	if profile != "poc" {
		return HumanListener{}, errors.New("human management HTTPS requires isolated loopback poc profile")
	}
	if cfg.Endpoint == "" || cfg.Issuer == "" || cfg.Audience == "" ||
		cfg.ClientID == "" || cfg.ClientSecretFile == "" ||
		cfg.ServerCert == "" || cfg.ServerKey == "" {
		return HumanListener{}, errors.New("incomplete human HTTPS access-token introspection configuration")
	}
	u, err := url.Parse(cfg.Issuer)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" {
		return HumanListener{}, errors.New("OAuth issuer must be a configured HTTPS authority")
	}
	cfg.Enabled = true
	return cfg, nil
}

func (c HumanListener) ReadClientSecret() (string, error) {
	data, err := os.ReadFile(c.ClientSecretFile)
	if err != nil || len(data) == 0 || len(data) > 16384 {
		return "", errors.New("OAuth client secret file cannot be loaded")
	}
	secret := strings.TrimSpace(string(data))
	if secret == "" {
		return "", errors.New("OAuth client secret file is empty")
	}
	return secret, nil
}
