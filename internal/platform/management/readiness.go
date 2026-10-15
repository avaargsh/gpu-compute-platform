package management

import (
	"context"
	"net/http"
	"time"
)

// readiness deliberately exposes no DB identity, grant, SQL, tenant or
// provider details. A failed authority check must not be mistaken for a
// healthy management control plane by a local rollout supervisor.
func (s *Service) readiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	var err error
	if s.RequireRuntimeDBAuthority {
		err = VerifyRuntimeDBPrivileges(ctx, s.DB)
	} else {
		err = s.DB.PingContext(ctx)
	}
	if err != nil {
		problem(w, http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
