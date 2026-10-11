// managementctl is a loopback-only engineering client for the isolated
// Management API. It is NOT an OIDC login provider or a production web UI.
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const baseURL = "http://127.0.0.1:8081"

var safeSegment = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)
var safeOperationKey = regexp.MustCompile(`^[0-9a-f]{32}$`)

type options struct {
	Action      string
	Cluster     string
	Project     string
	Kind        string
	Resource    string
	OperationID string
	File        string
	Generation  int64
}

func validate(o options) error {
	if o.Action == "new-key" {
		return nil
	}
	if !safeSegment.MatchString(o.Cluster) || !safeSegment.MatchString(o.Project) {
		return errors.New("cluster and project must be safe nonempty IDs")
	}
	switch o.Action {
	case "receipt":
		if !safeOperationKey.MatchString(o.OperationID) {
			return errors.New("receipt requires a 32-character lowercase hexadecimal operation ID")
		}
	case "get", "put", "delete":
		if !safeSegment.MatchString(o.Resource) || (o.Kind != "ComputePool" && o.Kind != "Workload") {
			return errors.New("resource ID and kind (ComputePool/Workload) are required")
		}
		if o.Action == "get" {
			return nil
		}
		if !safeOperationKey.MatchString(o.OperationID) {
			return errors.New("mutations require the original 128-bit Idempotency-Key (use -action new-key)")
		}
		if o.Action == "put" && o.File == "" {
			return errors.New("PUT requires a JSON request file with expectedGeneration")
		}
		if o.Action == "delete" && o.Generation < 1 {
			return errors.New("DELETE requires the exact existing Generation (If-Match)")
		}
	default:
		return errors.New("action must be new-key, receipt, get, put, or delete")
	}
	return nil
}

func newOperationKey() (string, error) {
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(key[:]), nil
}

func endpoint(o options) string {
	prefix := baseURL + "/api/v1/management/clusters/" + o.Cluster + "/projects/" + o.Project
	if o.Action == "receipt" {
		return prefix + "/operations/" + o.OperationID
	}
	return prefix + "/resources/" + o.Kind + "/" + o.Resource
}

func run(o options, out, stderr io.Writer) error {
	if err := validate(o); err != nil {
		return err
	}
	if o.Action == "new-key" {
		key, err := newOperationKey()
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, key)
		return err
	}
	token := os.Getenv("MANAGEMENT_ACCESS_TOKEN")
	if len(token) < 8 || strings.ContainsAny(token, " \r\n\t") {
		return errors.New("set MANAGEMENT_ACCESS_TOKEN to a valid OIDC access token in the current shell; never supply tokens as CLI arguments")
	}
	method := http.MethodGet
	var body io.Reader
	if o.Action == "put" {
		method = http.MethodPut
		info, err := os.Stat(o.File)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 1<<18 {
			return errors.New("request JSON must be a regular file no larger than 256 KiB")
		}
		data, err := os.ReadFile(o.File)
		if err != nil {
			return err
		}
		if !json.Valid(data) {
			return errors.New("request file is not valid JSON")
		}
		body = bytes.NewReader(data)
	} else if o.Action == "delete" {
		method = http.MethodDelete
	}
	req, err := http.NewRequest(method, endpoint(o), body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if o.Action == "put" || o.Action == "delete" {
		req.Header.Set("Idempotency-Key", o.OperationID)
		// Log the operation ID before sending. Reuse it for any retry or
		// receipt lookup if the transport drops the response.
		fmt.Fprintf(stderr, "operationId=%s (retain this ID for retries and receipt lookup)\n", o.OperationID)
	}
	if o.Action == "put" {
		req.Header.Set("Content-Type", "application/json")
	} else if o.Action == "delete" {
		req.Header.Set("If-Match", fmt.Sprintf("%q", strconv.FormatInt(o.Generation, 10)))
	}
	// No proxy, cookies, redirect-following, or arbitrary remote URL. This
	// tool must never send a Bearer access token outside 127.0.0.1.
	client := &http.Client{
		Timeout:       15 * time.Second,
		Transport:     &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request outcome unknown; query receipt, then retry with original operation ID: %w", err)
	}
	defer resp.Body.Close()
	fmt.Fprintf(out, "HTTP %d\n", resp.StatusCode)
	if id := resp.Header.Get("X-Request-ID"); id != "" {
		fmt.Fprintf(out, "X-Request-ID: %s\n", id)
	}
	if tag := resp.Header.Get("ETag"); tag != "" {
		fmt.Fprintf(out, "ETag: %s\n", tag)
	}
	result, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return err
	}
	if len(result) > 0 {
		fmt.Fprint(out, string(result))
		if result[len(result)-1] != '\n' {
			fmt.Fprintln(out)
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server returned HTTP %d; do not generate a new key for a retry", resp.StatusCode)
	}
	return nil
}

func main() {
	var o options
	flag.StringVar(&o.Action, "action", "", "new-key | get | put | delete | receipt")
	flag.StringVar(&o.Cluster, "cluster", "", "cluster ID")
	flag.StringVar(&o.Project, "project", "", "project ID")
	flag.StringVar(&o.Kind, "kind", "", "ComputePool or Workload")
	flag.StringVar(&o.Resource, "resource", "", "resource ID")
	flag.StringVar(&o.OperationID, "operation-id", "", "original 128-bit lowercase-hex operation ID")
	flag.StringVar(&o.File, "file", "", "PUT JSON request file")
	flag.Int64Var(&o.Generation, "generation", 0, "DELETE exact If-Match Generation")
	flag.Parse()
	if err := run(o, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "managementctl:", err)
		os.Exit(1)
	}
}
