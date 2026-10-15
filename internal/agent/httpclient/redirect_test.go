package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
)

// Agent protocol responses must not redirect an authenticated, replayable
// state update to another endpoint. A custom caller-supplied redirect policy
// must not weaken this guarantee.
func TestAgentProtocolNeverForwardsRedirects(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		call   func(*Client) error
	}{
		{
			name:   "desired-get-302",
			status: http.StatusFound,
			call: func(c *Client) error {
				_, err := c.PullDesired(context.Background(), "cluster-a")
				return err
			},
		},
		{
			name:   "register-post-307",
			status: http.StatusTemporaryRedirect,
			call: func(c *Client) error {
				return c.Register(context.Background(), agent.Registration{ClusterID: "cluster-a"})
			},
		},
		{
			name:   "report-post-308",
			status: http.StatusPermanentRedirect,
			call: func(c *Client) error {
				return c.Report(context.Background(), "cluster-a", nil)
			},
		},
		{
			name:   "lease-post-json-307",
			status: http.StatusTemporaryRedirect,
			call: func(c *Client) error {
				_, err := c.ClaimReconcileLease(context.Background(), agent.ReconcileLeaseRequest{
					ClusterID: "cluster-a", Kind: "Workload", ResourceID: "job-a",
					Owner: "agent-a", TTLSeconds: 15,
				})
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reachedDestination atomic.Int64
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				reachedDestination.Add(1)
				w.WriteHeader(http.StatusNoContent)
			}))
			defer destination.Close()

			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, destination.URL+"/unexpected-agent-protocol", tc.status)
			}))
			defer origin.Close()

			// Even a deliberately permissive injected client must be fenced.
			shared := origin.Client()
			shared.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return nil }
			c := New(origin.URL, shared)
			if err := tc.call(c); err == nil {
				t.Fatal("Agent protocol accepted a redirect response")
			}
			if n := reachedDestination.Load(); n != 0 {
				t.Fatalf("forwarded Agent request to redirect destination %d times", n)
			}
			// The client's original policy belongs to the caller and must not
			// be modified by the Agent wrapper.
			resp, err := shared.Get(origin.URL + "/test-caller-policy")
			if err != nil {
				t.Fatalf("Agent wrapper modified caller redirect policy: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent || reachedDestination.Load() != 1 {
				t.Fatalf("caller redirect policy was modified: status=%d redirects=%d", resp.StatusCode, reachedDestination.Load())
			}
		})
	}
}
