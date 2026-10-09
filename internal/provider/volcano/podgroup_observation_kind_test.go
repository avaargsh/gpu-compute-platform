package volcano

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

// This test is READ-ONLY and opt-in. The temp-runner fixture creates the
// disposable Job/PodGroup and must tear them down itself; no writes here.
// Running ordinary go test ./... skips live cluster access by design.
func TestVolcanoLivePodGroupLinkReadOnly(t *testing.T) {
	ns := os.Getenv("STAGE_B_LIVE_PODGROUP_NAMESPACE")
	contextName := os.Getenv("STAGE_B_LIVE_PODGROUP_CONTEXT")
	if ns == "" && contextName == "" {
		t.Skip("requires explicit disposable kind+Volcano fixture")
	}
	if contextName != "kind-stageb-volcano" ||
		!strings.HasPrefix(ns, "stageb-proof-") ||
		len(strings.TrimPrefix(ns, "stageb-proof-")) == 0 {
		t.Fatal("live PodGroup proof requires the fixed kind context and ephemeral namespace")
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules, &clientcmd.ConfigOverrides{CurrentContext: contextName},
	).ClientConfig()
	if err != nil {
		t.Fatal(err)
	}
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	job, err := client.Resource(volcanoJobGVR).Namespace(ns).
		Get(ctx, "stageb-proof-job", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if job.GetUID() == "" || job.GetResourceVersion() == "" {
		t.Fatal("real Job has no server UID/resourceVersion")
	}
	provider, err := NewProvider(client)
	if err != nil {
		t.Fatal(err)
	}
	link, err := provider.observeOwnedPodGroupLink(ctx, job, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if link.Type != "PodGroupLinked" || link.Status != "True" ||
		link.Reason != "VolcanoPodGroupControllerLinkVerified" {
		t.Fatalf("real API-server PodGroup link unproven: %#v", link)
	}
	group, err := client.Resource(volcanoPodGroupGVR).Namespace(ns).
		Get(ctx, job.GetName()+"-"+string(job.GetUID()), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Negative control is entirely local: no resource is ever mutated.
	replaced := group.DeepCopy()
	replaced.SetUID("foreign-replacement")
	if podGroupOwnedByJob(replaced, job) {
		t.Fatal("changed UID must not be accepted as the observed PodGroup")
	}
	t.Logf("LIVE_READ_ONLY_PODGROUP_LINK=PASS jobUID=%s podGroupUID=%s podGroupRV=%s",
		job.GetUID(), group.GetUID(), group.GetResourceVersion())
}
