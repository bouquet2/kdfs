package main

import (
	"context"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestEnsureNQNConfigCreatesConfigMapWhenMissing(t *testing.T) {
	ctx := context.Background()
	cl := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	if err := ensureNQNConfig(ctx, cl, "kdfs"); err != nil {
		t.Fatal(err)
	}
	var cm corev1.ConfigMap
	if err := cl.Get(ctx, client.ObjectKey{Namespace: "kdfs", Name: "kdfs-config"}, &cm); err != nil {
		t.Fatal(err)
	}
	if cm.Data["nqnAuthority"] != "nqn.2026-05.krea.to" {
		t.Fatalf("nqnAuthority = %q", cm.Data["nqnAuthority"])
	}
	if cm.Data["networkPolicy"] != defaultNetworkPolicyJSON() {
		t.Fatalf("networkPolicy = %q", cm.Data["networkPolicy"])
	}
}

func TestEnsureNQNConfigPreservesUserValue(t *testing.T) {
	ctx := context.Background()
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "kdfs-config", Namespace: "kdfs"}, Data: map[string]string{"nqnAuthority": "nqn.2026-05.cluster-b.example"}}
	cl := fake.NewClientBuilder().WithScheme(testScheme(t)).WithRuntimeObjects(cm).Build()
	if err := ensureNQNConfig(ctx, cl, "kdfs"); err != nil {
		t.Fatal(err)
	}
	var updated corev1.ConfigMap
	if err := cl.Get(ctx, client.ObjectKeyFromObject(cm), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Data["nqnAuthority"] != "nqn.2026-05.cluster-b.example" {
		t.Fatalf("nqnAuthority = %q", updated.Data["nqnAuthority"])
	}
}

func TestEnsureNQNConfigBackfillsMissingOrEmptyKey(t *testing.T) {
	for _, data := range []map[string]string{nil, {}, {"nqnAuthority": ""}} {
		ctx := context.Background()
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "kdfs-config", Namespace: "kdfs"}, Data: data}
		cl := fake.NewClientBuilder().WithScheme(testScheme(t)).WithRuntimeObjects(cm).Build()
		if err := ensureNQNConfig(ctx, cl, "kdfs"); err != nil {
			t.Fatal(err)
		}
		var updated corev1.ConfigMap
		if err := cl.Get(ctx, client.ObjectKeyFromObject(cm), &updated); err != nil {
			t.Fatal(err)
		}
		if updated.Data["nqnAuthority"] != "nqn.2026-05.krea.to" {
			t.Fatalf("nqnAuthority = %q", updated.Data["nqnAuthority"])
		}
	}
}

func TestEnsureNQNConfigCreatesDefaultNetworkPolicy(t *testing.T) {
	for _, data := range []map[string]string{
		nil,
		{},
		{"nqnAuthority": "nqn.2026-05.cluster-b.example"},
		{"nqnAuthority": "nqn.2026-05.cluster-b.example", "networkPolicy": ""},
		{"nqnAuthority": "nqn.2026-05.cluster-b.example", "networkPolicy": "   "},
	} {
		ctx := context.Background()
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "kdfs-config", Namespace: "kdfs"}, Data: data}
		cl := fake.NewClientBuilder().WithScheme(testScheme(t)).WithRuntimeObjects(cm).Build()
		if err := ensureNQNConfig(ctx, cl, "kdfs"); err != nil {
			t.Fatal(err)
		}
		var updated corev1.ConfigMap
		if err := cl.Get(ctx, client.ObjectKeyFromObject(cm), &updated); err != nil {
			t.Fatal(err)
		}
		if updated.Data["networkPolicy"] != defaultNetworkPolicyJSON() {
			t.Fatalf("networkPolicy = %q", updated.Data["networkPolicy"])
		}
	}
}

func TestRuntimeNamespaceUsesEnvOverride(t *testing.T) {
	t.Setenv("POD_NAMESPACE", "custom-kdfs")
	if got := runtimeNamespace(); got != "custom-kdfs" {
		t.Fatalf("namespace = %q", got)
	}
}

func TestNodeAgentBaseURLUsesPodIP(t *testing.T) {
	ctx := context.Background()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "kdfs-node-agent-abc", Namespace: "kdfs", Labels: map[string]string{"app": "kdfs-node-agent"}},
		Spec:       corev1.PodSpec{NodeName: "worker-2"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.42.0.18"},
	}
	cl := fake.NewClientBuilder().WithScheme(testScheme(t)).WithRuntimeObjects(pod).Build()
	url, err := nodeAgentBaseURL(ctx, cl, "kdfs", "worker-2")
	if err != nil {
		t.Fatal(err)
	}
	if url != "http://10.42.0.18:9808" {
		t.Fatalf("url = %q", url)
	}
}

func TestNodeAgentBaseURLBracketsIPv6PodIP(t *testing.T) {
	ctx := context.Background()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "kdfs-node-agent-abc", Namespace: "kdfs", Labels: map[string]string{"app": "kdfs-node-agent"}},
		Spec:       corev1.PodSpec{NodeName: "worker-2"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "fd00::50"},
	}
	cl := fake.NewClientBuilder().WithScheme(testScheme(t)).WithRuntimeObjects(pod).Build()
	url, err := nodeAgentBaseURL(ctx, cl, "kdfs", "worker-2")
	if err != nil {
		t.Fatal(err)
	}
	if url != "http://[fd00::50]:9808" {
		t.Fatalf("url = %q", url)
	}
}

func TestNodeAgentBaseURLSkipsUnreadyMatchingPod(t *testing.T) {
	ctx := context.Background()
	unready := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "kdfs-node-agent-old", Namespace: "kdfs", Labels: map[string]string{"app": "kdfs-node-agent"}},
		Spec:       corev1.PodSpec{NodeName: "worker-2"},
		Status:     corev1.PodStatus{Phase: corev1.PodPending},
	}
	ready := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "kdfs-node-agent-new", Namespace: "kdfs", Labels: map[string]string{"app": "kdfs-node-agent"}},
		Spec:       corev1.PodSpec{NodeName: "worker-2"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.42.0.18"},
	}
	cl := fake.NewClientBuilder().WithScheme(testScheme(t)).WithRuntimeObjects(unready, ready).Build()
	url, err := nodeAgentBaseURL(ctx, cl, "kdfs", "worker-2")
	if err != nil {
		t.Fatal(err)
	}
	if url != "http://10.42.0.18:9808" {
		t.Fatalf("url = %q", url)
	}
}

func TestRuntimeNamespaceUsesServiceAccountFile(t *testing.T) {
	t.Setenv("POD_NAMESPACE", "")
	prev := serviceAccountNamespacePath
	defer func() { serviceAccountNamespacePath = prev }()
	path := t.TempDir() + "/namespace"
	if err := os.WriteFile(path, []byte("file-kdfs\n"), 0644); err != nil {
		t.Fatal(err)
	}
	serviceAccountNamespacePath = path
	if got := runtimeNamespace(); got != "file-kdfs" {
		t.Fatalf("namespace = %q", got)
	}
}

func TestRuntimeNamespaceFallsBackToDefault(t *testing.T) {
	t.Setenv("POD_NAMESPACE", "")
	prev := serviceAccountNamespacePath
	defer func() { serviceAccountNamespacePath = prev }()
	serviceAccountNamespacePath = t.TempDir() + "/missing"
	if got := runtimeNamespace(); got != kdfsNamespace {
		t.Fatalf("namespace = %q", got)
	}
}
