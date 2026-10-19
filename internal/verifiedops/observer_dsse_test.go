package verifiedops

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
)

func TestSignedObserverEnvelopeIsContextBoundButNotAnOutcomeVerdict(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	want := ObserverSubject{OperationID: "op-a", ClusterID: "cluster-a", ProjectID: "project-a", WorkloadID: "train", Generation: 2, JobUID: "uid-a", Nonce: "unique-1", ObservedAt: "2026-10-20T01:00:00Z"}
	statement := ObserverStatement{Type: ObserverStatementType, PredicateType: ObserverPredicateType, Predicate: want}
	statement.Subject = append(statement.Subject, struct {
		Name   string            `json:"name"`
		Digest map[string]string `json:"digest"`
	}{Name: "k8s-job:uid-a", Digest: map[string]string{"sha256": hex.EncodeToString(sha256.New().Sum(nil))}})
	payload, _ := json.Marshal(statement)
	makeEnv := func(b []byte, key string) []byte {
		e := SignedObserverEnvelope{PayloadType: ObserverPayloadType, Payload: base64.StdEncoding.EncodeToString(b), Signatures: []ObserverSignature{{KeyID: key, Sig: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, dssePAE(ObserverPayloadType, b)))}}}
		raw, _ := json.Marshal(e)
		return raw
	}
	trust := ObserverTrust{KeyID: "observer-pinned", PublicKey: pub}
	good := makeEnv(payload, trust.KeyID)
	if _, err := VerifyObserverEnvelope(good, trust, want); err != nil {
		t.Fatal(err)
	}
	other := want
	other.OperationID = "op-b"
	if _, err := VerifyObserverEnvelope(good, trust, other); !errors.Is(err, ErrObserverUntrusted) {
		t.Fatal("cross-operation replay accepted", err)
	}
	other = want
	other.JobUID = "recreated-uid"
	if _, err := VerifyObserverEnvelope(good, trust, other); !errors.Is(err, ErrObserverUntrusted) {
		t.Fatal("recreated job accepted", err)
	}
	if _, err := VerifyObserverEnvelope(makeEnv(payload, "wrong-key"), trust, want); !errors.Is(err, ErrObserverUntrusted) {
		t.Fatal("wrong signer accepted", err)
	}
	var tampered SignedObserverEnvelope
	_ = json.Unmarshal(good, &tampered)
	tampered.Payload = base64.StdEncoding.EncodeToString(append(payload, ' '))
	broken, _ := json.Marshal(tampered)
	if _, err := VerifyObserverEnvelope(broken, trust, want); !errors.Is(err, ErrObserverUntrusted) {
		t.Fatal("tampered payload accepted", err)
	}
}
