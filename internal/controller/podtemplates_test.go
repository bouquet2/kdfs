package controller

import (
	"strings"
	"testing"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEnginePodTemplate(t *testing.T) {
	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: "pvc-1234-engine", Namespace: "kdfs"},
		Spec: storagev1alpha1.EngineSpec{
			NodeID: "worker-1",
			Replicas: []storagev1alpha1.ReplicaAttachment{
				{Name: "pvc-1234-replica-1", NodeID: "worker-2", NQN: "nqn.replica", Address: "10.0.0.6", Port: "4421"},
			},
		},
	}
	pod := EnginePodFor(engine, []string{})
	if pod.Spec.NodeName != "worker-1" {
		t.Fatalf("node = %q", pod.Spec.NodeName)
	}
	if len(pod.Spec.Containers) != 3 {
		t.Fatalf("containers = %d (expected 3: spdk-engine, sidecar, nfs)", len(pod.Spec.Containers))
	}
	if pod.Spec.Containers[0].Name != "spdk-engine" || pod.Spec.Containers[2].Name != "nfs" {
		t.Fatalf("containers = %#v", pod.Spec.Containers)
	}
	if pod.Spec.Containers[0].Image != "ghcr.io/bouquet2/kdfs/kdfs-spdk:dev" {
		t.Fatalf("spdk image = %q", pod.Spec.Containers[0].Image)
	}
	if pod.Spec.Containers[1].Image != "ghcr.io/bouquet2/kdfs/kdfs-sidecar:dev" {
		t.Fatalf("sidecar image = %q", pod.Spec.Containers[1].Image)
	}
	if pod.Spec.Containers[2].Image != "ghcr.io/bouquet2/kdfs/kdfs-nfs-sidecar:dev" {
		t.Fatalf("nfs image = %q", pod.Spec.Containers[2].Image)
	}
	var kdfsReplicas string
	for _, e := range pod.Spec.Containers[1].Env {
		if e.Name == "KDFS_REPLICAS" {
			kdfsReplicas = e.Value
		}
	}
	if !strings.Contains(kdfsReplicas, "nqn.replica") || !strings.Contains(kdfsReplicas, "10.0.0.6") {
		t.Fatalf("KDFS_REPLICAS = %q", kdfsReplicas)
	}
	assertNQNAuthorityEnv(t, pod.Spec.Containers[1].Env)
	assertRuntimeEnv(t, pod.Spec.Containers[1].Env)
	if pod.Spec.Containers[0].SecurityContext == nil || pod.Spec.Containers[0].SecurityContext.Privileged == nil || !*pod.Spec.Containers[0].SecurityContext.Privileged {
		t.Fatalf("spdk-engine container must be privileged")
	}
	if pod.Spec.Containers[1].SecurityContext != nil && pod.Spec.Containers[1].SecurityContext.Privileged != nil && *pod.Spec.Containers[1].SecurityContext.Privileged {
		t.Fatalf("sidecar container must not be privileged")
	}
	if pod.Spec.Containers[2].SecurityContext == nil || pod.Spec.Containers[2].SecurityContext.Privileged == nil || !*pod.Spec.Containers[2].SecurityContext.Privileged {
		t.Fatalf("nfs container must be privileged")
	}
}

func TestReplicaPodTemplate(t *testing.T) {
	replica := &storagev1alpha1.Replica{
		ObjectMeta: metav1.ObjectMeta{Name: "pvc-1234-replica-0", Namespace: "kdfs"},
		Spec:       storagev1alpha1.ReplicaSpec{NodeID: "worker-2", VolumeRef: storagev1alpha1.LocalObjectReference{Name: "pvc-1234"}},
	}
	pod := ReplicaPodFor(replica)
	if pod.Spec.NodeName != "worker-2" {
		t.Fatalf("node = %q", pod.Spec.NodeName)
	}
	if pod.Spec.Containers[0].Name != "spdk-replica" {
		t.Fatalf("container = %q", pod.Spec.Containers[0].Name)
	}
	if pod.Spec.Containers[0].Image != "ghcr.io/bouquet2/kdfs/kdfs-spdk:dev" {
		t.Fatalf("spdk image = %q", pod.Spec.Containers[0].Image)
	}
	if pod.Spec.Containers[1].Image != "ghcr.io/bouquet2/kdfs/kdfs-sidecar:dev" {
		t.Fatalf("sidecar image = %q", pod.Spec.Containers[1].Image)
	}
	var idx string
	for _, e := range pod.Spec.Containers[1].Env {
		if e.Name == "KDFS_REPLICA_INDEX" {
			idx = e.Value
		}
	}
	if idx != "0" {
		t.Fatalf("KDFS_REPLICA_INDEX = %q", idx)
	}
	assertNQNAuthorityEnv(t, pod.Spec.Containers[1].Env)
	assertRuntimeEnv(t, pod.Spec.Containers[1].Env)
	if pod.Spec.Containers[0].SecurityContext == nil || pod.Spec.Containers[0].SecurityContext.Privileged == nil || !*pod.Spec.Containers[0].SecurityContext.Privileged {
		t.Fatalf("spdk-replica container must be privileged")
	}
	if pod.Spec.Containers[1].SecurityContext != nil && pod.Spec.Containers[1].SecurityContext.Privileged != nil && *pod.Spec.Containers[1].SecurityContext.Privileged {
		t.Fatalf("replica sidecar container must not be privileged")
	}
}

func assertRuntimeEnv(t *testing.T, env []corev1.EnvVar) {
	t.Helper()
	assertFieldRefEnv(t, env, "KDFS_POD_IP", "status.podIP")
	assertFieldRefEnv(t, env, "KDFS_HOST_IP", "status.hostIP")
	assertFieldRefEnv(t, env, "KDFS_NODE_NAME", "spec.nodeName")
	for _, e := range env {
		if e.Name != "KDFS_NETWORK_POLICY" {
			continue
		}
		if e.ValueFrom == nil || e.ValueFrom.ConfigMapKeyRef == nil {
			t.Fatal("KDFS_NETWORK_POLICY must come from ConfigMap")
		}
		if e.ValueFrom.ConfigMapKeyRef.Name != "kdfs-config" || e.ValueFrom.ConfigMapKeyRef.Key != "networkPolicy" {
			t.Fatalf("KDFS_NETWORK_POLICY source = %#v", e.ValueFrom.ConfigMapKeyRef)
		}
		if e.ValueFrom.ConfigMapKeyRef.Optional == nil || !*e.ValueFrom.ConfigMapKeyRef.Optional {
			t.Fatalf("KDFS_NETWORK_POLICY should be optional during rollout: %#v", e.ValueFrom.ConfigMapKeyRef)
		}
		return
	}
	t.Fatal("KDFS_NETWORK_POLICY env not found")
}

func assertFieldRefEnv(t *testing.T, env []corev1.EnvVar, name, fieldPath string) {
	t.Helper()
	for _, e := range env {
		if e.Name != name {
			continue
		}
		if e.ValueFrom == nil || e.ValueFrom.FieldRef == nil {
			t.Fatalf("%s must come from fieldRef", name)
		}
		if e.ValueFrom.FieldRef.FieldPath != fieldPath {
			t.Fatalf("%s fieldPath = %q", name, e.ValueFrom.FieldRef.FieldPath)
		}
		return
	}
	t.Fatalf("%s env not found", name)
}

func TestEnginePodTemplateFiltersInvalidAllowedHosts(t *testing.T) {
	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: "pvc-1234-engine", Namespace: "kdfs"},
		Spec:       storagev1alpha1.EngineSpec{NodeID: "worker-1"},
	}
	pod := EnginePodFor(engine, []string{
		"nqn.2026-05.krea.to:krea.to:host-worker-1",
		"worker 2",
	})

	var allowedHosts string
	for _, e := range pod.Spec.Containers[1].Env {
		if e.Name == "KDFS_ALLOWED_HOSTS" {
			allowedHosts = e.Value
		}
	}
	if allowedHosts != "nqn.2026-05.krea.to:krea.to:host-worker-1" {
		t.Fatalf("KDFS_ALLOWED_HOSTS = %q", allowedHosts)
	}
}

func assertNQNAuthorityEnv(t *testing.T, env []corev1.EnvVar) {
	t.Helper()
	for _, e := range env {
		if e.Name != "NQN_AUTHORITY" {
			continue
		}
		if e.ValueFrom == nil || e.ValueFrom.ConfigMapKeyRef == nil {
			t.Fatal("NQN_AUTHORITY must come from ConfigMap")
		}
		if e.ValueFrom.ConfigMapKeyRef.Name != "kdfs-config" || e.ValueFrom.ConfigMapKeyRef.Key != "nqnAuthority" {
			t.Fatalf("NQN_AUTHORITY source = %#v", e.ValueFrom.ConfigMapKeyRef)
		}
		return
	}
	t.Fatal("NQN_AUTHORITY env not found")
}
