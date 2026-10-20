package verifiedops

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func workTicketFixture(t *testing.T) (ObserverWorkTicket, []byte, ObserverTrust, ed25519.PrivateKey) {
	t.Helper()
	w, _, _, _ := fixture()
	now := time.Now().UTC()
	ticket := ObserverWorkTicket{
		Challenge: ObserverSubject{
			OperationID: strings.Repeat("a", 32), ClusterID: "cluster", ProjectID: "research", WorkloadID: "workload-a",
			Generation: w.Generation, JobUID: string(w.JobUID), Nonce: strings.Repeat("b", 64),
		},
		Expected:  w,
		IssuedAt:  now.Add(-time.Second).Format(time.RFC3339Nano),
		ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano),
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := AuthorObserverWorkTicket(ticket, ObserverSigner{KeyID: "control-plane-pinned", Signer: key})
	if err != nil {
		t.Fatal(err)
	}
	return ticket, raw, ObserverTrust{KeyID: "control-plane-pinned", PublicKey: pub}, key
}

func TestObserverTicketPinnedControlPlaneSignature(t *testing.T) {
	ticket, raw, trust, key := workTicketFixture(t)
	approved, err := ApproveObserverWorkTicket(raw, trust)
	if err != nil || approved.ticket != ticket {
		t.Fatalf("valid signed ticket not approved: %#v %v", approved, err)
	}
	for _, tt := range []struct {
		name   string
		mutate func(*ObserverTrust, *[]byte)
	}{
		{"wrong-key-id", func(root *ObserverTrust, _ *[]byte) { root.KeyID = "attacker" }},
		{"wrong-key-bytes", func(root *ObserverTrust, _ *[]byte) {
			pub, _, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			root.PublicKey = pub
		}},
		{"changed-payload", func(_ *ObserverTrust, raw *[]byte) {
			var env SignedObserverEnvelope
			if err := json.Unmarshal(*raw, &env); err != nil {
				t.Fatal(err)
			}
			env.Payload = base64.StdEncoding.EncodeToString([]byte("{}"))
			*raw, _ = json.Marshal(env)
		}},
		{"duplicate-envelope-key", func(_ *ObserverTrust, raw *[]byte) {
			*raw = append((*raw)[:len(*raw)-1], []byte(`,"payloadType":"application/vnd.gpupanel.observer-work+json"}`)...)
		}},
		{"unsigned-json", func(_ *ObserverTrust, raw *[]byte) { *raw, _ = json.Marshal(ticket) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root, bytes := trust, append([]byte(nil), raw...)
			tt.mutate(&root, &bytes)
			if _, err := ApproveObserverWorkTicket(bytes, root); !errors.Is(err, ErrObserverUntrusted) {
				t.Fatalf("untrusted work ticket admitted: %v", err)
			}
		})
	}
	invalid := ticket
	invalid.ExpiresAt = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	if _, err := AuthorObserverWorkTicket(invalid, ObserverSigner{KeyID: trust.KeyID, Signer: key}); !errors.Is(err, ErrObserverUntrusted) {
		t.Fatalf("expired ticket authored: %v", err)
	}
	invalid = ticket
	invalid.ExpiresAt = time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339Nano)
	if _, err := AuthorObserverWorkTicket(invalid, ObserverSigner{KeyID: trust.KeyID, Signer: key}); !errors.Is(err, ErrObserverUntrusted) {
		t.Fatalf("unbounded ticket authored: %v", err)
	}
	invalid = ticket
	invalid.Challenge.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := AuthorObserverWorkTicket(invalid, ObserverSigner{KeyID: trust.KeyID, Signer: key}); !errors.Is(err, ErrObserverUntrusted) {
		t.Fatalf("caller-pre-filled observedAt: %v", err)
	}
}

func fakeObserverIdentity(c *fake.Clientset, username string, err error) {
	c.PrependReactor("create", "selfsubjectreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
		if err != nil {
			return true, nil, err
		}
		return true, &authenticationv1.SelfSubjectReview{Status: authenticationv1.SelfSubjectReviewStatus{
			UserInfo: authenticationv1.UserInfo{Username: username},
		}}, nil
	})
}

func TestObserverRunnerRejectsForeignIdentityBeforeResourceReads(t *testing.T) {
	_, raw, trust, _ := workTicketFixture(t)
	approved, err := ApproveObserverWorkTicket(raw, trust)
	if err != nil {
		t.Fatal(err)
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_ = pub
	w, j, p, n := fixture()
	_ = w
	for _, tc := range []struct {
		name     string
		username string
		failure  error
	}{
		{"management-service-account", "system:serviceaccount:management:api", nil},
		{"cluster-admin", "kubernetes-admin", nil},
		{"auth-review-denied", "", errors.New("forbidden")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fake.NewSimpleClientset(j, p, n)
			fakeObserverIdentity(c, tc.username, tc.failure)
			proof, obs, err := RunApprovedObserverWork(context.Background(), c, approved, ObserverSigner{KeyID: "observer", Signer: key})
			if !errors.Is(err, ErrObserverUntrusted) || len(proof) != 0 || obs.Code != "" {
				t.Fatalf("foreign identity signed work: %#v %#v %v", proof, obs, err)
			}
			for _, a := range c.Actions() {
				if a.GetResource().Resource != "selfsubjectreviews" {
					t.Fatalf("unauthorized identity read Kubernetes: %s", a.GetResource().Resource)
				}
			}
		})
	}
}

func TestObserverRunnerSignsPendingWithDistinctIdentity(t *testing.T) {
	_, raw, trust, _ := workTicketFixture(t)
	approved, err := ApproveObserverWorkTicket(raw, trust)
	if err != nil {
		t.Fatal(err)
	}
	w, j, _, _ := fixture()
	c := fake.NewSimpleClientset(j)
	fakeObserverIdentity(c, ObserverIdentityUsername, nil)
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	envelope, observation, err := RunApprovedObserverWork(context.Background(), c, approved, ObserverSigner{KeyID: "observer-only", Signer: key})
	if err != nil || observation.Code != OutcomePending {
		t.Fatalf("pending observation incorrectly signed: %#v %v", observation, err)
	}
	proof, err := VerifyObservedJobEnvelope(envelope, ObserverTrust{KeyID: "observer-only", PublicKey: pub}, approved.ticket.Challenge, w)
	if err != nil || proof.Observation.Code != OutcomePending {
		t.Fatalf("pending was not roundtripped: %#v %v", proof, err)
	}
	for _, a := range c.Actions() {
		if a.GetVerb() != "get" && a.GetVerb() != "list" && a.GetResource().Resource != "selfsubjectreviews" {
			t.Fatalf("observer mutated Kubernetes: %v", a)
		}
	}
}

// A dispatcher-approved work ticket and valid Observer SA are still not an
// authorization verdict when the bound Job starts deleting during readback.
func TestObserverRunnerSignsJobDeletionRaceAsUnproven(t *testing.T) {
	ticket, raw, root, _ := workTicketFixture(t)
	approved, err := ApproveObserverWorkTicket(raw, root)
	if err != nil {
		t.Fatal(err)
	}
	_, job, pod, node := fixture()
	client := fake.NewSimpleClientset(job, pod, node)
	fakeObserverIdentity(client, ObserverIdentityUsername, nil)
	reads := 0
	client.PrependReactor("get", "jobs", func(_ ktesting.Action) (bool, runtime.Object, error) {
		reads++
		if reads == 2 {
			deleting := job.DeepCopy()
			now := metav1.Now()
			deleting.DeletionTimestamp = &now
			// Preserve the original RV to prove the deletion fence is required.
			return true, deleting, nil
		}
		return false, nil, nil
	})
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	proof, observed, err := RunApprovedObserverWork(context.Background(), client, approved,
		ObserverSigner{KeyID: "independent-observer", Signer: key})
	if err != nil || observed.Code != OwnershipUnproven || reads != 2 {
		t.Fatalf("deleting Job was signed as Ready: %#v %v reads=%d", observed, err, reads)
	}
	verified, err := VerifyObservedJobEnvelope(proof,
		ObserverTrust{KeyID: "independent-observer", PublicKey: pub},
		ticket.Challenge, ticket.Expected)
	if err != nil || verified.Observation.Code != OwnershipUnproven {
		t.Fatalf("signed deletion must remain non-positive: %#v %v", verified, err)
	}
}
