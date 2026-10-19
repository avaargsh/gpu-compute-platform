package verifiedops

import (
 "bytes"
 "crypto/ed25519"
 "crypto/sha256"
 "encoding/base64"
 "encoding/json"
 "errors"
 "fmt"
 "strings"
)

// SignedObserverEnvelope is a deliberately narrow, offline-verifiable
// DSSE v1 envelope. Signature validity authenticates a signer, NOT the
// Kubernetes observation or authorization.
type SignedObserverEnvelope struct {
 PayloadType string `json:"payloadType"`
 Payload string `json:"payload"`
 Signatures []ObserverSignature `json:"signatures"`
}
type ObserverSignature struct {
 KeyID string `json:"keyid"`
 Sig string `json:"sig"`
}
type ObserverSubject struct {
 OperationID string `json:"operationId"`
 ClusterID string `json:"clusterId"`
 ProjectID string `json:"projectId"`
 WorkloadID string `json:"workloadId"`
 Generation int64 `json:"generation"`
 JobUID string `json:"jobUid"`
 Nonce string `json:"nonce"`
 ObservedAt string `json:"observedAt"`
}
type ObserverStatement struct {
 Type string `json:"_type"`
 PredicateType string `json:"predicateType"`
 Subject []struct {
  Name string `json:"name"`
  Digest map[string]string `json:"digest"`
 } `json:"subject"`
 Predicate ObserverSubject `json:"predicate"`
}
type ObserverTrust struct { KeyID string; PublicKey ed25519.PublicKey }
var ErrObserverUntrusted=errors.New("observer signature or context untrusted")
const ObserverPayloadType="application/vnd.in-toto+json"
const ObserverStatementType="https://in-toto.io/Statement/v1"
const ObserverPredicateType="https://gpupanel.dev/attestation/observer/v1"

func dssePAE(kind string, payload []byte) []byte {
 return []byte(fmt.Sprintf("DSSEv1 %d %s %d %s",len(kind),kind,len(payload),payload))
}

// VerifyObserverEnvelope validates a pinned Ed25519 trust root, DSSE
// pre-authentication encoding, exact operation context and the Statement
// shape. It is intentionally not an authorization or outcome verdict.
// Callers must separately validate freshness, nonce uniqueness, original
// CREATE ACK ownership and independent Kubernetes reads.
func VerifyObserverEnvelope(raw []byte, trust ObserverTrust, want ObserverSubject)(ObserverStatement,error){
 var out ObserverStatement
 if len(raw)==0||len(raw)>1<<20||trust.KeyID==""||len(trust.PublicKey)!=ed25519.PublicKeySize {
  return out,ErrObserverUntrusted
 }
 var env SignedObserverEnvelope
 if err:=json.Unmarshal(raw,&env);err!=nil{return out,ErrObserverUntrusted}
 if env.PayloadType!=ObserverPayloadType||len(env.Signatures)!=1||env.Signatures[0].KeyID!=trust.KeyID{
  return out,ErrObserverUntrusted
 }
 payload,err:=base64.StdEncoding.Strict().DecodeString(env.Payload)
 if err!=nil||len(payload)==0||len(payload)>1<<19{return out,ErrObserverUntrusted}
 signature,err:=base64.StdEncoding.Strict().DecodeString(env.Signatures[0].Sig)
 if err!=nil||!ed25519.Verify(trust.PublicKey,dssePAE(env.PayloadType,payload),signature){
  return out,ErrObserverUntrusted
 }
 decoder:=json.NewDecoder(bytes.NewReader(payload))
 decoder.DisallowUnknownFields()
 if err:=decoder.Decode(&out);err!=nil{return ObserverStatement{},ErrObserverUntrusted}
 if out.Type!=ObserverStatementType||out.PredicateType!=ObserverPredicateType||len(out.Subject)!=1||
  len(out.Subject[0].Digest)!=1||!strings.HasPrefix(out.Subject[0].Name,"k8s-job:") {
  return ObserverStatement{},ErrObserverUntrusted
 }
 p:=out.Predicate
 if p!=want||p.OperationID==""||p.ClusterID==""||p.ProjectID==""||p.WorkloadID==""||
  p.Generation<1||p.JobUID==""||p.Nonce==""||p.ObservedAt==""{
  return ObserverStatement{},ErrObserverUntrusted
 }
 if out.Subject[0].Name!="k8s-job:"+p.JobUID{return ObserverStatement{},ErrObserverUntrusted}
 digest:=out.Subject[0].Digest["sha256"]
 if len(digest)!=64||strings.Trim(digest,"0123456789abcdef")!=""{return ObserverStatement{},ErrObserverUntrusted}
 // Reject a second JSON document appended after a signed statement.
 var extra any
 if decoder.Decode(&extra)==nil{return ObserverStatement{},ErrObserverUntrusted}
 return out,nil
}
var _=sha256.Size
