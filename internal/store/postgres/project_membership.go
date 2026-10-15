package postgres

import (
	"context"
	"database/sql"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/platform/authn"
)

// CanReadProject verifies server-side authorization on *every request*.
// OAuth tokens never carry authority to choose their own Project or role.
// The pair (issuer, sub) prevents subjects from different IdPs colliding.
func (s *Store) CanReadProject(ctx context.Context, principal authn.Principal, projectID domain.ID) (bool, error) {
	if s.db == nil || principal.Issuer == "" || principal.Subject == "" || projectID == "" {
		return false, sql.ErrConnDone
	}
	var allowed bool
	err := s.db.QueryRowContext(ctx, `
SELECT EXISTS(
	SELECT 1 FROM platform_project_memberships
	WHERE issuer = $1 AND subject = $2 AND project_id = $3
	  AND active = TRUE AND role IN ('owner','developer','viewer')
)`, principal.Issuer, principal.Subject, projectID).Scan(&allowed)
	return allowed, err
}
