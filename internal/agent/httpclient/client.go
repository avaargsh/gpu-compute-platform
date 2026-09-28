package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
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
	resp, err := c.client.Do(req)
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
	resp, err := c.client.Do(req)
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
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("control plane returned %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
