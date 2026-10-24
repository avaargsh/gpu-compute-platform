package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
)

type Client struct {
	baseURL string
	client  *http.Client
}

func New(baseURL string, client *http.Client) *Client {
	if client == nil {
		client = http.DefaultClient
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), client: client}
}

// doRequest never follows redirects. A Control Plane response must not
// re-route Agent protocol requests, especially 307/308 which replay POST
// bodies, to any second endpoint. Clone the caller's client: other consumers
// may legitimately need its original redirect policy.
func (c *Client) doRequest(req *http.Request) (*http.Response, error) {
	noRedirect := *c.client
	noRedirect.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return noRedirect.Do(req)
}

func (c *Client) Register(ctx context.Context, registration agent.Registration) error {
	return c.post(ctx, "/api/v1/agent/register", registration)
}

func (c *Client) Heartbeat(ctx context.Context, heartbeat agent.Heartbeat) error {
	return c.post(ctx, "/api/v1/agent/heartbeat", heartbeat)
}

func (c *Client) PullDesired(ctx context.Context, clusterID domain.ID) ([]agent.DesiredResource, error) {
	var out []agent.DesiredResource
	path := "/api/v1/agent/desired?clusterId=" + url.QueryEscape(string(clusterID))
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) ClaimReconcileLease(ctx context.Context, in agent.ReconcileLeaseRequest) (bool, error) {
	var out agent.ReconcileLeaseResponse
	if err := c.postJSON(ctx, "/api/v1/agent/reconcile-lease/claim", in, &out); err != nil {
		return false, err
	}
	return out.Claimed, nil
}

func (c *Client) ReleaseReconcileLease(ctx context.Context, in agent.ReconcileLeaseRequest) error {
	return c.post(ctx, "/api/v1/agent/reconcile-lease/release", in)
}

func (c *Client) FinalizeDesired(ctx context.Context, in agent.FinalizeDesiredRequest) error {
	return c.post(ctx, "/api/v1/agent/finalize-desired", in)
}

// PrepareJobCreateAttempt is mandatory before any API-server Job CREATE.
func (c *Client) PrepareJobCreateAttempt(ctx context.Context, in verifiedops.JobCreateAttempt) error {
	return c.post(ctx, "/api/v1/agent/workload-create-attempt", in)
}

// ResolveJobCreateAttempt reads the persisted outcome before the Agent touches
// the provider again. UNKNOWN is not permission to repeat the external CREATE.
func (c *Client) ResolveJobCreateAttempt(
	ctx context.Context,
	operationID string,
	clusterID domain.ID,
	workloadID domain.ID,
	generation int64,
) (verifiedops.JobCreateAttemptResolution, error) {
	values := url.Values{}
	values.Set("clusterId", string(clusterID))
	values.Set("operationId", operationID)
	values.Set("workloadId", string(workloadID))
	values.Set("generation", strconv.FormatInt(generation, 10))
	var out verifiedops.JobCreateAttemptResolution
	if err := c.get(ctx, "/api/v1/agent/workload-create-attempt?"+values.Encode(), &out); err != nil {
		return verifiedops.JobCreateAttemptResolution{}, err
	}
	switch out.State {
	case verifiedops.JobCreateAttemptNotFound, verifiedops.JobCreateAttemptUnknown, verifiedops.JobCreateAttemptStale:
		if out.Binding != nil {
			return verifiedops.JobCreateAttemptResolution{}, fmt.Errorf("control plane returned binding for non-bound CREATE state")
		}
	case verifiedops.JobCreateAttemptACKBound:
		binding := out.Binding
		if binding == nil || binding.OperationID != operationID || binding.ClusterID != clusterID ||
			binding.WorkloadID != workloadID || binding.Generation != generation ||
			binding.JobUID == "" || binding.JobResourceVersion == "" ||
			binding.Namespace == "" || binding.JobName == "" {
			return verifiedops.JobCreateAttemptResolution{}, fmt.Errorf("control plane returned incomplete CREATE ACK binding")
		}
	default:
		return verifiedops.JobCreateAttemptResolution{}, fmt.Errorf("control plane returned unsupported CREATE attempt state")
	}
	return out, nil
}

// PublishJobCreateUID sends a candidate CREATE ACK via the same no-redirect
// authenticated Agent transport. Never call it for GET/adopted resources.
func (c *Client) PublishJobCreateUID(ctx context.Context, in agent.JobCreateUIDCandidate) error {
	return c.post(ctx, "/api/v1/agent/workload-create-uid", in)
}

func (c *Client) Report(ctx context.Context, clusterID domain.ID, observations []agent.Observation) error {
	payload := struct {
		ClusterID    domain.ID           `json:"clusterId"`
		Observations []agent.Observation `json:"observations"`
	}{ClusterID: clusterID, Observations: observations}
	return c.post(ctx, "/api/v1/agent/report", payload)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.doRequest(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("control plane returned %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) post(ctx context.Context, path string, in any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.doRequest(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("control plane returned %s", resp.Status)
	}
	return nil
}

func (c *Client) postJSON(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.doRequest(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("control plane returned %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
