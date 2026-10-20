package postgres

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	fake "k8s.io/client-go/kubernetes/fake"
)

const v2Digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func pendingV2Proof(t *testing.T) (*Store, JobCreateUIDBinding, ObserverChallenge, []byte, verifiedops.ObserverTrust) {
	t.Helper()
	store,binding:=newCreateUIDFixture(t)
	ctx:=context.Background()
	if _,err:=store.db.ExecContext(ctx,`
UPDATE desired_resources SET spec=jsonb_set(spec,'{image}',to_jsonb($3::text),true)
WHERE cluster_id=$1 AND kind='Workload' AND resource_id=$2
`,binding.ClusterID,binding.WorkloadID,"registry.invalid/train@"+v2Digest);err!=nil{t.Fatal(err)}
	if err:=store.PrepareJobCreateAttempt(ctx,durableAttempt(binding));err!=nil{t.Fatal(err)}
	if err:=store.RecordJobCreateUID(ctx,binding);err!=nil{t.Fatal(err)}
	challenge,err:=store.IssueObserverChallenge(ctx,makeObserverScope(binding))
	if err!=nil{t.Fatal(err)}
	job:=&batchv1.Job{
		ObjectMeta:metav1.ObjectMeta{
			Name:binding.JobName,Namespace:binding.Namespace,UID:types.UID(binding.JobUID),
			ResourceVersion:"180",
			Annotations:map[string]string{"ai.compute/generation":"1"},
			Labels:map[string]string{"ai.compute/workload":binding.JobName},
		},
		Spec:batchv1.JobSpec{Template:corev1.PodTemplateSpec{Spec:corev1.PodSpec{
			Containers:[]corev1.Container{{Name:"workload",Image:"registry.invalid/train@"+v2Digest}},
		}}},
	}
	subject:=verifiedops.ObserverSubject{
		OperationID:challenge.OperationID,ClusterID:string(challenge.ClusterID),
		ProjectID:challenge.ProjectID,WorkloadID:string(challenge.WorkloadID),
		Generation:challenge.Generation,JobUID:challenge.JobUID,Nonce:challenge.Nonce,
	}
	expected:=verifiedops.ExpectedJob{
		Namespace:binding.Namespace,Name:binding.JobName,JobUID:types.UID(binding.JobUID),
		Generation:binding.Generation,ContainerName:"workload",ImageDigest:v2Digest,
	}
	pub,priv,err:=ed25519.GenerateKey(rand.Reader)
	if err!=nil{t.Fatal(err)}
	raw,observed,err:=verifiedops.NewJobObserver(fake.NewSimpleClientset(job)).ObserveAndSign(
		ctx,subject,expected,verifiedops.ObserverSigner{KeyID:"pinned-test-only",Signer:priv})
	if err!=nil||observed.Code!=verifiedops.OutcomePending{
		t.Fatalf("signed pending fixture: %#v %v",observed,err)
	}
	return store,binding,challenge,raw,verifiedops.ObserverTrust{KeyID:"pinned-test-only",PublicKey:pub}
}

func TestSignedObservedJobV2DurablePendingAndCrossVersionReplay(t *testing.T){
	store,binding,challenge,raw,trust:=pendingV2Proof(t)
	ctx:=context.Background()
	scope:=makeObserverScope(binding)
	receipt,err:=store.RecordSignedObservedJobProof(ctx,raw,trust,scope,challenge.Nonce)
	if err!=nil||receipt.Observation.Code!=verifiedops.OutcomePending{
		t.Fatalf("v2 evidence not accepted as pending: %#v %v",receipt,err)
	}
	if receipt.Observation.Code=="AUTHORIZED_AND_VERIFIED"{t.Fatal("receipt falsely authorized execution")}
	retry,err:=New(store.db).RecordSignedObservedJobProof(ctx,raw,trust,scope,challenge.Nonce)
	if err!=nil||retry.EnvelopeSHA256!=receipt.EnvelopeSHA256{
		t.Fatalf("durable retry not idempotent: %#v %v",retry,err)
	}
	var count int
	var version,code string
	err=store.db.QueryRowContext(ctx,`
SELECT count(*),max(predicate_type),max(observation_json->>'Code')
FROM verified_observer_nonce_receipts WHERE cluster_id=$1 AND nonce=$2
`,binding.ClusterID,challenge.Nonce).Scan(&count,&version,&code)
	if err!=nil||count!=1||version!=verifiedops.ObservedJobPredicateType||code!=verifiedops.OutcomePending{
		t.Fatalf("v2 receipt corrupted: count=%d version=%s code=%s err=%v",count,version,code,err)
	}
	legacySubject:=subjectForChallenge(challenge)
	legacyRaw,legacyTrust:=signedNonceFixture(t,legacySubject)
	if err:=store.RecordSignedObserverNonce(ctx,legacyRaw,legacyTrust,legacySubject);
		!errors.Is(err,ErrObserverEvidenceReplayed){
		t.Fatalf("v1 reused v2 nonce: %v",err)
	}
}

func TestSignedObservedJobV2RejectsForgedScopeAndUnissuedChallenge(t *testing.T){
	store,binding,challenge,raw,trust:=pendingV2Proof(t)
	ctx:=context.Background()
	scope:=makeObserverScope(binding)
	wrong:=scope
	wrong.ProjectID="other-project"
	if _,err:=store.RecordSignedObservedJobProof(ctx,raw,trust,wrong,challenge.Nonce);
		!errors.Is(err,ErrObserverNonceUnproven){t.Fatalf("wrong tenant accepted: %v",err)}
	wrong=scope
	wrong.JobUID="replacement-uid"
	if _,err:=store.RecordSignedObservedJobProof(ctx,raw,trust,wrong,challenge.Nonce);
		!errors.Is(err,ErrObserverNonceUnproven){t.Fatalf("same-name new UID accepted: %v",err)}
	if _,err:=store.RecordSignedObservedJobProof(ctx,raw,trust,scope,strings.Repeat("f",64));
		!errors.Is(err,ErrObserverChallengeMissing){t.Fatalf("unissued nonce accepted: %v",err)}
	var count int
	if err:=store.db.QueryRowContext(ctx,"SELECT count(*) FROM verified_observer_nonce_receipts").Scan(&count);
		err!=nil||count!=0{t.Fatalf("bad evidence consumed nonce: %d %v",count,err)}
	if _,err:=store.RecordSignedObservedJobProof(ctx,raw,trust,scope,challenge.Nonce);err!=nil{
		t.Fatalf("rejected attempts poisoned legitimate challenge: %v",err)
	}
}
