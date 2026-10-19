package kueue

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	batchtyped "k8s.io/client-go/kubernetes/typed/batch/v1"
	"k8s.io/client-go/tools/clientcmd"
)

// This is deliberately a real Kubernetes API-server test. Client-go fake
// trackers don't generate UIDs/RVs and cannot enforce DELETE preconditions.
// The CI job provisions kind + Kueue before setting VERIFIEDOPS_KIND_E2E.
type kindInterceptClient struct {
	kubernetes.Interface
	raceName string
	gate <-chan struct{}
	joined *sync.WaitGroup
	lostACKName string
}
type kindInterceptBatch struct {
	batchtyped.BatchV1Interface
	intercept *kindInterceptClient
}
type kindInterceptJobs struct {
	batchtyped.JobInterface
	intercept *kindInterceptClient
}
func (c *kindInterceptClient) BatchV1() batchtyped.BatchV1Interface {
	return &kindInterceptBatch{BatchV1Interface:c.Interface.BatchV1(),intercept:c}
}
func (c *kindInterceptBatch) Jobs(namespace string) batchtyped.JobInterface {
	return &kindInterceptJobs{JobInterface:c.BatchV1Interface.Jobs(namespace),intercept:c.intercept}
}
func (j *kindInterceptJobs) Get(ctx context.Context, name string, options metav1.GetOptions) (*batchv1.Job,error) {
	obj,err:=j.JobInterface.Get(ctx,name,options)
	if name==j.intercept.raceName && apierrors.IsNotFound(err) {
		j.intercept.joined.Done()
		select {
		case <-j.intercept.gate:
		case <-ctx.Done():return nil,ctx.Err()
		}
	}
	return obj,err
}
func (j *kindInterceptJobs) Create(ctx context.Context, job *batchv1.Job, options metav1.CreateOptions) (*batchv1.Job,error) {
	created,err:=j.JobInterface.Create(ctx,job,options)
	if err==nil && job.Name==j.intercept.lostACKName {
		// The write succeeded against the real API server. Simulate only the
		// network acknowledgement disappearing, never invent a returned UID.
		return nil,apierrors.NewTimeoutError("injected lost response after real CREATE",1)
	}
	return created,err
}

func kindJob(ns,name string) Job {
	return Job{
		Namespace:ns,Name:name,Generation:7,Image:"busybox:1.36",
		Command:[]string{"sh","-c","sleep 60"},
		Resources:map[string]int64{"example.com/gpu":1},
	}
}

func TestRealKindAPIServerJobCREATEOwnershipBoundaries(t *testing.T) {
	if os.Getenv("VERIFIEDOPS_KIND_E2E")!="1" {
		t.Skip("requires real kind/Kueue cluster and VERIFIEDOPS_KIND_E2E=1")
	}
	config,err:=clientcmd.BuildConfigFromFlags("",os.Getenv("KUBECONFIG"))
	if err!=nil{t.Fatal(err)}
	config.QPS=20
	config.Burst=40
	core,err:=kubernetes.NewForConfig(config)
	if err!=nil{t.Fatal(err)}
	ctx,cancel:=context.WithTimeout(context.Background(),2*time.Minute)
	defer cancel()
	ns,err:=core.CoreV1().Namespaces().Create(ctx,&corev1.Namespace{
		ObjectMeta:metav1.ObjectMeta{GenerateName:"verifiedops-kind-"},
	},metav1.CreateOptions{})
	if err!=nil{t.Fatal(err)}
	defer func(){
		cleanup,cancel:=context.WithTimeout(context.Background(),20*time.Second)
		defer cancel()
		if err:=core.CoreV1().Namespaces().Delete(cleanup,ns.Name,metav1.DeleteOptions{});err!=nil && !apierrors.IsNotFound(err){
			t.Logf("namespace cleanup: %v",err)
		}
	}()
	client:=NewKubeClient(core,nil)
	// Exact CREATE ACK is the only UID/RV candidate allowed to enter a
	// durable operation binding. Later GET is never an equivalent receipt.
	workload:=kindJob(ns.Name,"job-ack")
	ack,err:=client.ApplyJobWithCreateResponse(ctx,workload)
	if err!=nil||ack.Disposition!=JobCreateAcknowledged||ack.UID==""||ack.ResourceVersion==""{
		t.Fatalf("real API CREATE ACK missing UID/RV: %#v err %v",ack,err)
	}
	live,err:=core.BatchV1().Jobs(ns.Name).Get(ctx,workload.Name,metav1.GetOptions{})
	if err!=nil||live.UID!=ack.UID||live.ResourceVersion==""{
		t.Fatalf("API server CREATE identity mismatch: %#v err %v",live,err)
	}
	replayed,err:=client.ApplyJobWithCreateResponse(ctx,workload)
	if err!=nil||replayed.Disposition!=JobExistingUnbound||replayed.UID!=""||replayed.ResourceVersion!=""{
		t.Fatalf("GET replay minted CREATE provenance: %#v %v",replayed,err)
	}

	// A stale operation's UID must never authenticate a new Job after
	// deletion and same-name recreation, even with matching Generation.
	oldUID:=ack.UID
	if err:=core.BatchV1().Jobs(ns.Name).Delete(ctx,workload.Name,metav1.DeleteOptions{});err!=nil{t.Fatal(err)}
	if err:=wait.PollUntilContextTimeout(ctx,250*time.Millisecond,20*time.Second,true,func(ctx context.Context)(bool,error){
		_,err:=core.BatchV1().Jobs(ns.Name).Get(ctx,workload.Name,metav1.GetOptions{})
		if apierrors.IsNotFound(err) { return true,nil }
		return false,err
	});err!=nil{t.Fatal(err)}
	replacement,err:=client.ApplyJobWithCreateResponse(ctx,workload)
	if err!=nil||replacement.Disposition!=JobCreateAcknowledged||replacement.UID==oldUID{
		t.Fatalf("same-name recreated Job reused identity: %#v %v",replacement,err)
	}
	out,err:=verifiedops.NewJobObserver(core).Observe(ctx,verifiedops.ExpectedJob{
		Namespace:ns.Name,Name:workload.Name,JobUID:oldUID,Generation:workload.Generation,
		ContainerName:"workload",ImageDigest:"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err!=nil||out.Code!=verifiedops.OwnershipUnproven{
		t.Fatalf("old bound UID incorrectly attributed to replacement: %#v err %v",out,err)
	}
	err=core.BatchV1().Jobs(ns.Name).Delete(ctx,workload.Name,metav1.DeleteOptions{Preconditions:&metav1.Preconditions{UID:&oldUID}})
	if !apierrors.IsConflict(err){
		t.Fatalf("API-server should fence stale UID DELETE after same-name recreation: %v",err)
	}
	current,err:=core.BatchV1().Jobs(ns.Name).Get(ctx,workload.Name,metav1.GetOptions{})
	if err!=nil||current.UID!=replacement.UID{
		t.Fatalf("UID race deleted replacement: %#v %v",current,err)
	}

	// The two racing agents both read real 404 before either can CREATE.
	// Kubernetes, not fake reactors, picks exactly one successful writer.
	race:=kindJob(ns.Name,"job-race")
	gate:=make(chan struct{})
	var joined sync.WaitGroup
	joined.Add(2)
	results:=make(chan JobCreateResponse,2)
	errorsSeen:=make(chan error,2)
	for i:=0;i<2;i++{
		intercept:=&kindInterceptClient{Interface:core,raceName:race.Name,gate:gate,joined:&joined}
		go func(){
			result,err:=NewKubeClient(intercept,nil).ApplyJobWithCreateResponse(ctx,race)
			results<-result
			errorsSeen<-err
		}()
	}
	synced:=make(chan struct{})
	go func(){joined.Wait();close(synced)}()
	select {case <-synced:close(gate);case <-ctx.Done():close(gate);t.Fatal(ctx.Err())}
	acknowledged,adopted:=0,0
	var winningUID types.UID
	for i:=0;i<2;i++{
		result,err:=<-results
		if err!=nil{t.Fatalf("real concurrent CREATE: %v",err)}
		if result.Disposition==JobCreateAcknowledged && result.UID!="" && result.ResourceVersion!=""{
			acknowledged++
			winningUID=result.UID
		}else if result.Disposition==JobRaceAdoptedUnbound && result.UID=="" && result.ResourceVersion==""{
			adopted++
		}else{
			t.Fatalf("race result gave false CREATE proof: %#v",result)
		}
	}
	for i:=0;i<2;i++{if err:=<-errorsSeen;err!=nil{t.Fatal(err)}}
	if acknowledged!=1||adopted!=1{t.Fatalf("race winners wrong: ACK=%d unbound=%d",acknowledged,adopted)}
	raceLive,err:=core.BatchV1().Jobs(ns.Name).Get(ctx,race.Name,metav1.GetOptions{})
	if err!=nil||raceLive.UID!=winningUID{t.Fatalf("actual race winner missing: %#v %v",raceLive,err)}

	// Inject an ACK loss *after* a real successful API-server CREATE. The
	// re-GET must converge without manufacturing that lost CREATE response.
	lost:=kindJob(ns.Name,"job-lost-ack")
	fault:=NewKubeClient(&kindInterceptClient{Interface:core,lostACKName:lost.Name},nil)
	if got,err:=fault.ApplyJobWithCreateResponse(ctx,lost);!apierrors.IsTimeout(err)||got.UID!=""{
		t.Fatalf("injected real-write lost ACK was promoted: %#v %v",got,err)
	}
	stored,err:=core.BatchV1().Jobs(ns.Name).Get(ctx,lost.Name,metav1.GetOptions{})
	if err!=nil||stored.UID==""{t.Fatalf("real API CREATE did not persist: %#v %v",stored,err)}
	unknown,err:=client.ApplyJobWithCreateResponse(ctx,lost)
	if err!=nil||unknown.Disposition!=JobExistingUnbound||unknown.UID!=""{
		t.Fatalf("lost ACK recovery falsely proves CREATE: %#v %v",unknown,err)
	}
	t.Logf("real API-server tests passed in namespace %s (Kueue installed); unsigned observations only",ns.Name)
}
