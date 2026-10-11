package llmd

import (
    "context"
    "fmt"

    apierrors "k8s.io/apimachinery/pkg/api/errors"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/runtime/schema"
    "k8s.io/apimachinery/pkg/util/validation"
    "k8s.io/client-go/discovery"
    "k8s.io/client-go/dynamic"
)

const groupVersion = "inference.networking.k8s.io/v1"

var poolGVR = schema.GroupVersionResource{
    Group: "inference.networking.k8s.io", Version: "v1", Resource: "inferencepools",
}

// Result is a read-only Gateway API Inference Extension (GAIE) observation.
// An InferencePool may be managed by llm-d or another conformant implementation.
// Observing it proves neither llm-d ownership nor serving health or GPU admission.
type Result struct {
    APIVersion string `json:"apiVersion"`
    Namespace string `json:"namespace"`
    Name string `json:"name"`
    APIServed bool `json:"apiServed"`
    ObjectObserved bool `json:"objectObserved"`
    UID string `json:"uid,omitempty"`
    ResourceVersion string `json:"resourceVersion,omitempty"`
    Generation int64 `json:"generation,omitempty"`
    Status string `json:"status"`
    Reason string `json:"reason"`
    ServingReadyProven bool `json:"servingReadyProven"`
}

// Probe observes a named GAIE v1 InferencePool. It never mutates Kubernetes,
// registers a provider, or promotes InferencePool existence into serving readiness.
func Probe(
    ctx context.Context,
    discover discovery.DiscoveryInterface,
    resources dynamic.Interface,
    namespace, name string,
) (Result, error) {
    result := Result{
        APIVersion: groupVersion,
        Namespace: namespace,
        Name: name,
        Status: "BLOCKED",
        Reason: "Unverified",
    }
    if len(validation.IsDNS1123Label(namespace)) > 0 ||
        len(validation.IsDNS1123Subdomain(name)) > 0 {
        result.Reason = "InvalidResourceIdentity"
        return result, fmt.Errorf("namespace and InferencePool name must be valid Kubernetes DNS names")
    }
    if discover == nil || resources == nil {
        result.Reason = "KubernetesClientsMissing"
        return result, fmt.Errorf("Kubernetes discovery and dynamic clients are required")
    }

    list, err := discover.ServerResourcesForGroupVersion(groupVersion)
    if err != nil {
        result.Reason = "InferencePoolAPIDiscoveryFailed"
        return result, fmt.Errorf("discover GAIE v1 API: %w", err)
    }
    if list == nil {
        result.Reason = "InferencePoolV1APINotServed"
        return result, nil
    }
    for _, api := range list.APIResources {
        if api.Name == poolGVR.Resource && api.Namespaced {
            result.APIServed = true
            break
        }
    }
    if !result.APIServed {
        result.Reason = "InferencePoolV1APINotServed"
        return result, nil
    }

    pool, err := resources.Resource(poolGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
    if apierrors.IsNotFound(err) {
        result.Reason = "InferencePoolNotFound"
        return result, nil
    }
    if err != nil {
        result.Reason = "InferencePoolReadFailed"
        return result, fmt.Errorf("read InferencePool %s/%s: %w", namespace, name, err)
    }
    if pool == nil ||
        pool.GetAPIVersion() != groupVersion ||
        pool.GetKind() != "InferencePool" ||
        pool.GetNamespace() != namespace ||
        pool.GetName() != name ||
        pool.GetUID() == "" ||
        pool.GetResourceVersion() == "" ||
        pool.GetGeneration() <= 0 ||
        pool.GetDeletionTimestamp() != nil {
        result.Reason = "InferencePoolIdentityUncertain"
        return result, nil
    }

    result.ObjectObserved = true
    result.UID = string(pool.GetUID())
    result.ResourceVersion = pool.GetResourceVersion()
    result.Generation = pool.GetGeneration()
    result.Status = "OBSERVED_ONLY"
    result.Reason = "GAIEInferencePoolObservedNotServingProof"
    return result, nil
}
