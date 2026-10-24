package bootstrap

import (
	"errors"
	"strings"
)

// AgentListener contains only file paths and trust-domain configuration, never
// key bytes. It is intentionally restricted to a loopback-only PoC profile.
// ListenAddr enables a separate Agent-only listener so a local operator API can
// remain on its own loopback listener without exposing it through mTLS routes.
type AgentListener struct {
	Enabled     bool
	ListenAddr  string
	Separate    bool
	ServerCert  string
	ServerKey   string
	ClientCA    string
	TrustDomain string
}

func LoadAgentListener(getenv func(string) string, profile string) (AgentListener, error) {
	cfg := AgentListener{
		ListenAddr:  strings.TrimSpace(getenv("CONTROL_PLANE_AGENT_LISTEN_ADDR")),
		ServerCert:  strings.TrimSpace(getenv("CONTROL_PLANE_TLS_CERT")),
		ServerKey:   strings.TrimSpace(getenv("CONTROL_PLANE_TLS_KEY")),
		ClientCA:    strings.TrimSpace(getenv("CONTROL_PLANE_AGENT_CLIENT_CA")),
		TrustDomain: strings.TrimSpace(getenv("CONTROL_PLANE_AGENT_TRUST_DOMAIN")),
	}
	if cfg.ListenAddr == "" && cfg.ServerCert == "" && cfg.ServerKey == "" && cfg.ClientCA == "" && cfg.TrustDomain == "" {
		return AgentListener{}, nil
	}
	// The legacy single-listener mTLS profile remains PoC-only. Development may
	// opt into mTLS only for a second loopback listener; its original listener
	// is independently constrained to loopback by bootstrap.Load. The PoC
	// profile must not combine mTLS Agent routes with an anonymous main API.
	if cfg.ListenAddr != "" && profile != "development" {
		return AgentListener{}, errors.New("separate Agent mTLS listener requires the loopback-only development profile")
	}
	if profile != "poc" && !(profile == "development" && cfg.ListenAddr != "") {
		return AgentListener{}, errors.New("Agent mTLS listener requires the loopback-only poc profile or a dedicated development listener")
	}
	if cfg.ServerCert == "" || cfg.ServerKey == "" || cfg.ClientCA == "" || cfg.TrustDomain == "" {
		return AgentListener{}, errors.New("incomplete Agent mTLS listener configuration")
	}
	if cfg.ListenAddr != "" {
		if err := validateLoopback(cfg.ListenAddr); err != nil {
			return AgentListener{}, errors.New("CONTROL_PLANE_AGENT_LISTEN_ADDR must be a loopback host:port")
		}
		cfg.Separate = true
	}
	cfg.Enabled = true
	return cfg, nil
}
