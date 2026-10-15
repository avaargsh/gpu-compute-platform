package bootstrap

import (
	"errors"
	"strings"
)

// AgentListener contains only file paths and trust-domain configuration, never
// key bytes. It is intentionally restricted to the loopback-only PoC profile.
type AgentListener struct {
	Enabled     bool
	ServerCert  string
	ServerKey   string
	ClientCA    string
	TrustDomain string
}

func LoadAgentListener(getenv func(string) string, profile string) (AgentListener, error) {
	cfg := AgentListener{
		ServerCert:  strings.TrimSpace(getenv("CONTROL_PLANE_TLS_CERT")),
		ServerKey:   strings.TrimSpace(getenv("CONTROL_PLANE_TLS_KEY")),
		ClientCA:    strings.TrimSpace(getenv("CONTROL_PLANE_AGENT_CLIENT_CA")),
		TrustDomain: strings.TrimSpace(getenv("CONTROL_PLANE_AGENT_TRUST_DOMAIN")),
	}
	if cfg.ServerCert == "" && cfg.ServerKey == "" && cfg.ClientCA == "" && cfg.TrustDomain == "" {
		return AgentListener{}, nil
	}
	if profile != "poc" {
		return AgentListener{}, errors.New("Agent mTLS listener requires loopback-only poc profile")
	}
	if cfg.ServerCert == "" || cfg.ServerKey == "" || cfg.ClientCA == "" || cfg.TrustDomain == "" {
		return AgentListener{}, errors.New("incomplete Agent mTLS listener configuration")
	}
	cfg.Enabled = true
	return cfg, nil
}
