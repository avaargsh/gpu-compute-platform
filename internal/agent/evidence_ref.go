package agent

import (
	"fmt"
	"net/url"
	"strings"
)

const AIFactoryEvidenceScheme = "aifactory"

// AIFactoryEvidenceRef keeps acceptance evidence owned by AI Factory while the
// compute control plane stores only a stable reference on its Observation.
func AIFactoryEvidenceRef(bundleID string) (string, error) {
	bundleID = strings.TrimSpace(bundleID)
	if bundleID == "" || strings.Contains(bundleID, "/") {
		return "", fmt.Errorf("invalid AI Factory evidence bundle id %q", bundleID)
	}
	return "aifactory://evidence/" + url.PathEscape(bundleID), nil
}

func ParseAIFactoryEvidenceRef(ref string) (string, error) {
	u, err := url.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("parse AI Factory evidence ref: %w", err)
	}
	if u.Scheme != AIFactoryEvidenceScheme || u.Host != "evidence" {
		return "", fmt.Errorf("unsupported AI Factory evidence ref %q", ref)
	}
	bundleID := strings.TrimPrefix(u.EscapedPath(), "/")
	if bundleID == "" || strings.Contains(bundleID, "/") {
		return "", fmt.Errorf("invalid AI Factory evidence ref %q", ref)
	}
	bundleID, err = url.PathUnescape(bundleID)
	if err != nil || strings.TrimSpace(bundleID) == "" {
		return "", fmt.Errorf("invalid AI Factory evidence ref %q", ref)
	}
	return bundleID, nil
}
