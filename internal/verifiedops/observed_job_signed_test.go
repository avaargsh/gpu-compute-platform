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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func signedChallengeFixture(w ExpectedJob) ObserverSubject {
	return ObserverSubject{OperationID:strings.Repeat("a",32),ClusterID:"cluster-a",
		ProjectID:"research",WorkloadID:"workload-a",Generation:w.Generation,
		JobUID:string(w.JobUID),Nonce:strings.Repeat("f",64)}
}

func TestSignedReadyObservationDigestAndAuthorityContext(t *testing.T){
	w,j,p,n:=fixture()
	client:=fake.NewSimpleClientset(j,p,n)
	pub,priv,err:=ed25519.GenerateKey(rand.Reader)
	if err!=nil{t.Fatal(err)}
	challenge:=signedChallengeFixture(w)
	raw,observed,err:=NewJobObserver(client).ObserveAndSign(context.Background(),challenge,w,
		ObserverSigner{KeyID:"observer-pinned",Signer:priv})
	if err!=nil||observed.Code!=PODReadyObserved||observed.NodeResourceVersion!=n.ResourceVersion{
		t.Fatalf("signed read failed: %#v err %v",observed,err)
	}
	trust:=ObserverTrust{KeyID:"observer-pinned",PublicKey:pub}
	proof,err:=VerifyObservedJobEnvelope(raw,trust,challenge,w)
	if err!=nil||proof.Observation.Code!=PODReadyObserved||proof.Subject.ObservedAt==""{
		t.Fatalf("signed proof was not bound: %#v %v",proof,err)
	}
	for _,action:=range client.Actions(){
		if action.GetVerb()!="get"&&action.GetVerb()!="list"{t.Fatalf("observer wrote Kubernetes: %v",action)}
	}
	tests:=[]struct{
		name string
		mutate func(*ObserverSubject,*ExpectedJob,*ObserverTrust)
	}{
		{"operation-replay",func(c *ObserverSubject,_ *ExpectedJob,_ *ObserverTrust){c.OperationID=strings.Repeat("b",32)}},
		{"tenant-replay",func(c *ObserverSubject,_ *ExpectedJob,_ *ObserverTrust){c.ProjectID="foreign"}},
		{"generation-drift",func(c *ObserverSubject,_ *ExpectedJob,_ *ObserverTrust){c.Generation++}},
		{"UID-replacement",func(c *ObserverSubject,w *ExpectedJob,_ *ObserverTrust){c.JobUID="other";w.JobUID="other"}},
		{"signer-mismatch",func(_ *ObserverSubject,_ *ExpectedJob,trust *ObserverTrust){trust.KeyID="other"}},
	}
	for _,tc:=range tests{t.Run(tc.name,func(t *testing.T){
		c,e,k:=challenge,w,trust
		tc.mutate(&c,&e,&k)
		if _,err:=VerifyObservedJobEnvelope(raw,k,c,e);!errors.Is(err,ErrObserverUntrusted){
			t.Fatalf("cross-boundary evidence accepted: %v",err)
		}
	})}
	var envelope SignedObserverEnvelope
	if err:=json.Unmarshal(raw,&envelope);err!=nil{t.Fatal(err)}
	envelope.Payload=base64.StdEncoding.EncodeToString([]byte("tampered"))
	broken,_:=json.Marshal(envelope)
	if _,err:=VerifyObservedJobEnvelope(broken,trust,challenge,w);!errors.Is(err,ErrObserverUntrusted){
		t.Fatalf("tampered signed evidence accepted: %v",err)
	}
}

func TestSignedPendingAndUnavailableRemainNonPositive(t *testing.T){
	w,j,p,n:=fixture()
	p.Status.Conditions[0].Status=corev1.ConditionFalse
	pub,priv,err:=ed25519.GenerateKey(rand.Reader)
	if err!=nil{t.Fatal(err)}
	challenge:=signedChallengeFixture(w)
	client:=fake.NewSimpleClientset(j,p,n)
	raw,obs,err:=NewJobObserver(client).ObserveAndSign(context.Background(),challenge,w,ObserverSigner{KeyID:"key",Signer:priv})
	if err!=nil||obs.Code!=OutcomePending{t.Fatalf("pending became success: %#v %v",obs,err)}
	got,err:=VerifyObservedJobEnvelope(raw,ObserverTrust{KeyID:"key",PublicKey:pub},challenge,w)
	if err!=nil||got.Observation.Code!=OutcomePending{t.Fatalf("pending sign verification: %#v %v",got,err)}
	_,negative,err:=NewJobObserver(fake.NewSimpleClientset()).ObserveAndSign(context.Background(),challenge,w,ObserverSigner{KeyID:"key",Signer:priv})
	if err==nil||negative.Code!=VerificationUnavailable{t.Fatalf("404 produced signed success: %#v %v",negative,err)}
	bad:=challenge;bad.Nonce="guessed"
	count:=len(client.Actions())
	if _,_,err:=NewJobObserver(client).ObserveAndSign(context.Background(),bad,w,ObserverSigner{KeyID:"key",Signer:priv});!errors.Is(err,ErrObserverUntrusted){
		t.Fatalf("invalid challenge accepted: %v",err)
	}
	if len(client.Actions())!=count{t.Fatal("invalid challenge touched API")}
}

func TestObserverNodeReplacedBetweenReadsCannotSignReady(t *testing.T){
	w,j,p,n:=fixture()
	client:=fake.NewSimpleClientset(j,p,n)
	calls:=0
	client.PrependReactor("get","nodes",func(a ktesting.Action)(bool,runtime.Object,error){
		calls++
		if calls==2{
			replaced:=n.DeepCopy()
			replaced.ResourceVersion="new-node-RV"
			return true,replaced,nil
		}
		return false,nil,nil
	})
	view,err:=NewJobObserver(client).Observe(context.Background(),w)
	if err!=nil||view.Code!=OwnershipUnproven||calls!=2{
		t.Fatalf("node replaced during evidence gathering: %#v %v calls=%d",view,err,calls)
	}
}

func TestSignedProofRejectsStaleTimestampAndChangedDigest(t *testing.T){
	w,j,p,n:=fixture()
	pub,priv,err:=ed25519.GenerateKey(rand.Reader);if err!=nil{t.Fatal(err)}
	challenge:=signedChallengeFixture(w)
	raw,_,err:=NewJobObserver(fake.NewSimpleClientset(j,p,n)).ObserveAndSign(context.Background(),challenge,w,ObserverSigner{KeyID:"key",Signer:priv})
	if err!=nil{t.Fatal(err)}
	var env SignedObserverEnvelope
	if err:=json.Unmarshal(raw,&env);err!=nil{t.Fatal(err)}
	decoded,err:=base64.StdEncoding.DecodeString(env.Payload);if err!=nil{t.Fatal(err)}
	var st ObservedJobStatement
	if err:=json.Unmarshal(decoded,&st);err!=nil{t.Fatal(err)}
	st.Predicate.Challenge.ObservedAt=time.Now().Add(-10*time.Minute).UTC().Format(time.RFC3339Nano)
	body,_:=json.Marshal(st)
	env.Payload=base64.StdEncoding.EncodeToString(body)
	env.Signatures[0].Sig=base64.StdEncoding.EncodeToString(ed25519.Sign(priv,dssePAE(ObserverPayloadType,body)))
	stale,_:=json.Marshal(env)
	if _,err:=VerifyObservedJobEnvelope(stale,ObserverTrust{KeyID:"key",PublicKey:pub},challenge,w);!errors.Is(err,ErrObserverUntrusted){
		t.Fatalf("stale but signed proof admitted: %v",err)
	}
	// A freshly signed statement whose predicate digest was not updated
	// must also fail without relying on a bad outer signature.
	st.Predicate.Challenge.ObservedAt=time.Now().UTC().Format(time.RFC3339Nano)
	st.Predicate.Observation.PodUID="invented-pod"
	body,_=json.Marshal(st)
	env.Payload=base64.StdEncoding.EncodeToString(body)
	env.Signatures[0].Sig=base64.StdEncoding.EncodeToString(ed25519.Sign(priv,dssePAE(ObserverPayloadType,body)))
	changed,_:=json.Marshal(env)
	if _,err:=VerifyObservedJobEnvelope(changed,ObserverTrust{KeyID:"key",PublicKey:pub},challenge,w);!errors.Is(err,ErrObserverUntrusted){
		t.Fatalf("signed body changed without updating subject digest: %v",err)
	}
}
