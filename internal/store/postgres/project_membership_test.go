package postgres

import (
	"context"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/platform/authn"
)

// This test uses the same real PostgreSQL test database as the lifecycle
// contracts; the membership lookup is not mocked.
func TestPostgresProjectMembershipAuthorityAndRevocation(t *testing.T) {
	db := openContractDB(t)
	ctx := context.Background()
	store := New(db)
	principal := authn.Principal{Issuer: "https://issuer.example/test", Subject: "verified-sub-s0"}
	projectID := domain.ID("project-membership-s0")
	_, err := db.ExecContext(ctx, `
INSERT INTO platform_project_memberships(issuer, subject, project_id, role, active)
VALUES($1, $2, $3, 'viewer', TRUE)
ON CONFLICT (issuer, subject, project_id) DO UPDATE
SET role = 'viewer', active = TRUE, updated_at = now()
`, principal.Issuer, principal.Subject, projectID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(),
			"DELETE FROM platform_project_memberships WHERE issuer=$1 AND subject=$2 AND project_id=$3",
			principal.Issuer, principal.Subject, projectID)
	})
	for _, tc := range []struct {
		name    string
		user    authn.Principal
		project domain.ID
		want    bool
	}{
		{"member", principal, projectID, true},
		{"foreign-issuer", authn.Principal{Issuer: "https://another.example", Subject: principal.Subject}, projectID, false},
		{"foreign-subject", authn.Principal{Issuer: principal.Issuer, Subject: "unrelated-user"}, projectID, false},
		{"foreign-project", principal, "project-other", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := store.CanReadProject(ctx, tc.user, tc.project)
			if err != nil || got != tc.want {
				t.Fatalf("CanReadProject=%t err=%v want=%t", got, err, tc.want)
			}
		})
	}
	if _, err := db.ExecContext(ctx, `
UPDATE platform_project_memberships SET active = FALSE
WHERE issuer=$1 AND subject=$2 AND project_id=$3
`, principal.Issuer, principal.Subject, projectID); err != nil {
		t.Fatal(err)
	}
	allowed, err := store.CanReadProject(ctx, principal, projectID)
	if err != nil || allowed {
		t.Fatalf("revoked user read access: allowed=%t err=%v", allowed, err)
	}
}
