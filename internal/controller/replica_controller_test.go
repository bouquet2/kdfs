package controller

import (
	"context"
	"errors"
	"strings"
	"testing"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	"github.com/bouquet2/kdfs/internal/agent"
	"github.com/bouquet2/kdfs/internal/names"
	"github.com/bouquet2/kdfs/internal/sidecar"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestReplicaReconcileLocalCreatesReplicaAndMarksRunning(t *testing.T) {
	ctx := context.Background()
	replica := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 0), Namespace: "kdfs"}, Spec: storagev1alpha1.ReplicaSpec{VolumeRef: storagev1alpha1.LocalObjectReference{Name: "pvc-1234"}, NodeID: "worker-1", Type: storagev1alpha1.ReplicaTypeLocal, Size: "10Gi", DataPath: names.DataPath("pvc-1234")}}
	fakeAgent := &agent.FakeClient{}
	c := newFakeClient(t, replica).WithStatusSubresource(&storagev1alpha1.Replica{}).Build()
	r := &ReplicaReconciler{Client: c, Scheme: testScheme(t), Agent: fakeAgent}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: replica.Name, Namespace: replica.Namespace}})
	if err != nil {
		t.Fatal(err)
	}
	updated := &storagev1alpha1.Replica{}
	if err := c.Get(ctx, types.NamespacedName{Name: replica.Name, Namespace: replica.Namespace}, updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status.Phase != storagev1alpha1.ReplicaPhaseRunning || updated.Status.BdevName == "" {
		t.Fatalf("status = %#v", updated.Status)
	}
}

func TestReplicaReconcileRemoteCreatesPodAndMarksRunning(t *testing.T) {
	ctx := context.Background()
	replica := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 1), Namespace: "kdfs"}, Spec: storagev1alpha1.ReplicaSpec{VolumeRef: storagev1alpha1.LocalObjectReference{Name: "pvc-1234"}, NodeID: "worker-2", Type: storagev1alpha1.ReplicaTypeRemote, Size: "10Gi", DataPath: names.DataPath("pvc-1234")}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: replica.Name + "-pod", Namespace: "kdfs", Labels: map[string]string{"kdfs.krea.to/mode": "replica"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.244.1.25", ContainerStatuses: []corev1.ContainerStatus{{Name: "sidecar", Ready: true}}}}
	c := newFakeClient(t, replica, pod).WithStatusSubresource(&storagev1alpha1.Replica{}).Build()
	r := &ReplicaReconciler{Client: c, Scheme: testScheme(t), Agent: &agent.FakeClient{}, SidecarStatusClient: &fakeSidecarStatusClient{status: sidecar.Status{Role: "replica", ReplicaNQN: names.ReplicaNQN("pvc-1234", 1), Endpoint: "worker-2:4421"}}}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: replica.Name, Namespace: replica.Namespace}})
	if err != nil {
		t.Fatal(err)
	}
	updated := &storagev1alpha1.Replica{}
	if err := c.Get(ctx, types.NamespacedName{Name: replica.Name, Namespace: replica.Namespace}, updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status.NQN != names.ReplicaNQN("pvc-1234", 1) || updated.Status.Endpoint != "worker-2:4421" {
		t.Fatalf("status = %#v", updated.Status)
	}
}

func TestReplicaReconcilePublishesIPv6EndpointFromSidecarStatus(t *testing.T) {
	ctx := context.Background()
	replica := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 1), Namespace: "kdfs"}, Spec: storagev1alpha1.ReplicaSpec{VolumeRef: storagev1alpha1.LocalObjectReference{Name: "pvc-1234"}, NodeID: "worker-2", Type: storagev1alpha1.ReplicaTypeRemote, Size: "10Gi", DataPath: names.DataPath("pvc-1234")}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: replica.Name + "-pod", Namespace: "kdfs", Labels: map[string]string{"kdfs.krea.to/mode": "replica"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "fd00::21", ContainerStatuses: []corev1.ContainerStatus{{Name: "sidecar", Ready: true}}}}
	c := newFakeClient(t, replica, pod).WithStatusSubresource(&storagev1alpha1.Replica{}).Build()
	statusClient := &fakeSidecarStatusClient{status: sidecar.Status{Role: "replica", ReplicaNQN: names.ReplicaNQN("pvc-1234", 1), Endpoint: "[fd00::21]:4421"}}
	r := &ReplicaReconciler{Client: c, Scheme: testScheme(t), Agent: &agent.FakeClient{}, SidecarStatusClient: statusClient}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: replica.Name, Namespace: replica.Namespace}})
	if err != nil {
		t.Fatal(err)
	}

	updated := &storagev1alpha1.Replica{}
	if err := c.Get(ctx, types.NamespacedName{Name: replica.Name, Namespace: replica.Namespace}, updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status.Endpoint != "[fd00::21]:4421" {
		t.Fatalf("endpoint = %q", updated.Status.Endpoint)
	}
	if updated.Status.NQN != names.ReplicaNQN("pvc-1234", 1) {
		t.Fatalf("nqn = %q", updated.Status.NQN)
	}
	if len(statusClient.podIPs) != 1 || statusClient.podIPs[0] != "fd00::21" {
		t.Fatalf("sidecar podIPs = %#v", statusClient.podIPs)
	}
}

func TestReplicaReconcileRequeuesOnMalformedSidecarStatus(t *testing.T) {
	ctx := context.Background()
	replica := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 1), Namespace: "kdfs"}, Spec: storagev1alpha1.ReplicaSpec{VolumeRef: storagev1alpha1.LocalObjectReference{Name: "pvc-1234"}, NodeID: "worker-2", Type: storagev1alpha1.ReplicaTypeRemote, Size: "10Gi", DataPath: names.DataPath("pvc-1234")}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: replica.Name + "-pod", Namespace: "kdfs", Labels: map[string]string{"kdfs.krea.to/mode": "replica"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "fd00::21", ContainerStatuses: []corev1.ContainerStatus{{Name: "sidecar", Ready: true}}}}
	c := newFakeClient(t, replica, pod).WithStatusSubresource(&storagev1alpha1.Replica{}).Build()
	r := &ReplicaReconciler{Client: c, Scheme: testScheme(t), Agent: &agent.FakeClient{}, SidecarStatusClient: &fakeSidecarStatusClient{status: sidecar.Status{Role: "engine", Endpoint: "[fd00::21]:4421"}}}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: replica.Name, Namespace: replica.Namespace}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Requeue && result.RequeueAfter == 0 {
		t.Fatalf("result = %#v, want requeue", result)
	}

	updated := &storagev1alpha1.Replica{}
	if err := c.Get(ctx, types.NamespacedName{Name: replica.Name, Namespace: replica.Namespace}, updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status.Phase == storagev1alpha1.ReplicaPhaseRunning {
		t.Fatalf("status should not be running: %#v", updated.Status)
	}
	if updated.Status.Endpoint != "" || updated.Status.NQN != "" {
		t.Fatalf("status should remain empty on malformed sidecar payload: %#v", updated.Status)
	}
}

func TestReplicaReconcileRequeuesOnMalformedSidecarEndpoint(t *testing.T) {
	ctx := context.Background()
	replica := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 1), Namespace: "kdfs"}, Spec: storagev1alpha1.ReplicaSpec{VolumeRef: storagev1alpha1.LocalObjectReference{Name: "pvc-1234"}, NodeID: "worker-2", Type: storagev1alpha1.ReplicaTypeRemote, Size: "10Gi", DataPath: names.DataPath("pvc-1234")}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: replica.Name + "-pod", Namespace: "kdfs", Labels: map[string]string{"kdfs.krea.to/mode": "replica"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "fd00::21", ContainerStatuses: []corev1.ContainerStatus{{Name: "sidecar", Ready: true}}}}
	c := newFakeClient(t, replica, pod).WithStatusSubresource(&storagev1alpha1.Replica{}).Build()
	r := &ReplicaReconciler{Client: c, Scheme: testScheme(t), Agent: &agent.FakeClient{}, SidecarStatusClient: &fakeSidecarStatusClient{status: sidecar.Status{Role: "replica", ReplicaNQN: names.ReplicaNQN("pvc-1234", 1), Endpoint: "fd00::21:4421"}}}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: replica.Name, Namespace: replica.Namespace}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Requeue && result.RequeueAfter == 0 {
		t.Fatalf("result = %#v, want requeue", result)
	}
}

func TestReplicaReconcileRequeuesOnEmptyPortSidecarEndpoint(t *testing.T) {
	ctx := context.Background()
	replica := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 1), Namespace: "kdfs"}, Spec: storagev1alpha1.ReplicaSpec{VolumeRef: storagev1alpha1.LocalObjectReference{Name: "pvc-1234"}, NodeID: "worker-2", Type: storagev1alpha1.ReplicaTypeRemote, Size: "10Gi", DataPath: names.DataPath("pvc-1234")}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: replica.Name + "-pod", Namespace: "kdfs", Labels: map[string]string{"kdfs.krea.to/mode": "replica"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "fd00::21", ContainerStatuses: []corev1.ContainerStatus{{Name: "sidecar", Ready: true}}}}
	c := newFakeClient(t, replica, pod).WithStatusSubresource(&storagev1alpha1.Replica{}).Build()
	r := &ReplicaReconciler{Client: c, Scheme: testScheme(t), Agent: &agent.FakeClient{}, SidecarStatusClient: &fakeSidecarStatusClient{status: sidecar.Status{Role: "replica", ReplicaNQN: names.ReplicaNQN("pvc-1234", 1), Endpoint: "worker-2:"}}}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: replica.Name, Namespace: replica.Namespace}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Requeue && result.RequeueAfter == 0 {
		t.Fatalf("result = %#v, want requeue", result)
	}
}

func TestReplicaReconcileErrorsWithoutAgentClient(t *testing.T) {
	ctx := context.Background()
	replica := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 0), Namespace: "kdfs"}, Spec: storagev1alpha1.ReplicaSpec{VolumeRef: storagev1alpha1.LocalObjectReference{Name: "pvc-1234"}, NodeID: "worker-1", Type: storagev1alpha1.ReplicaTypeLocal, Size: "10Gi", DataPath: names.DataPath("pvc-1234")}}
	c := newFakeClient(t, replica).WithStatusSubresource(&storagev1alpha1.Replica{}).Build()
	r := &ReplicaReconciler{Client: c, Scheme: testScheme(t)}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: replica.Name, Namespace: replica.Namespace}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "agent") {
		t.Fatalf("error = %v", err)
	}
}

func TestReplicaReconcileUsesAgentFactory(t *testing.T) {
	ctx := context.Background()
	replica := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 0), Namespace: "kdfs"}, Spec: storagev1alpha1.ReplicaSpec{VolumeRef: storagev1alpha1.LocalObjectReference{Name: "pvc-1234"}, NodeID: "worker-9", Type: storagev1alpha1.ReplicaTypeLocal, Size: "10Gi", DataPath: names.DataPath("pvc-1234")}}
	c := newFakeClient(t, replica).WithStatusSubresource(&storagev1alpha1.Replica{}).Build()
	called := false
	r := &ReplicaReconciler{Client: c, Scheme: testScheme(t), AgentFactory: func(ctx context.Context, nodeID string) (agent.Client, error) {
		called = true
		if nodeID != "worker-9" {
			t.Fatalf("nodeID = %q", nodeID)
		}
		return &agent.FakeClient{}, nil
	}}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: replica.Name, Namespace: replica.Namespace}})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected agent factory to be called")
	}
}

func TestReplicaReconcileReturnsErrorWhenProvisioningFails(t *testing.T) {
	ctx := context.Background()
	replica := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 0), Namespace: "kdfs"}, Spec: storagev1alpha1.ReplicaSpec{VolumeRef: storagev1alpha1.LocalObjectReference{Name: "pvc-1234"}, NodeID: "worker-1", Type: storagev1alpha1.ReplicaTypeLocal, Size: "10Gi", DataPath: names.DataPath("pvc-1234")}}
	c := newFakeClient(t, replica).WithStatusSubresource(&storagev1alpha1.Replica{}).Build()
	r := &ReplicaReconciler{Client: c, Scheme: testScheme(t), Agent: failingAgent{err: errors.New("boom")}}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: replica.Name, Namespace: replica.Namespace}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	updated := &storagev1alpha1.Replica{}
	if getErr := c.Get(ctx, types.NamespacedName{Name: replica.Name, Namespace: replica.Namespace}, updated); getErr != nil {
		t.Fatal(getErr)
	}
	if updated.Status.Phase == storagev1alpha1.ReplicaPhaseRunning {
		t.Fatalf("status should not be running: %#v", updated.Status)
	}
}

type failingAgent struct{ err error }

func (f failingAgent) CreateReplica(context.Context, agent.CreateReplicaRequest) (agent.CreateReplicaResponse, error) {
	return agent.CreateReplicaResponse{}, f.err
}

func (f failingAgent) DeleteReplica(context.Context, agent.DeleteReplicaRequest) error { return f.err }

func (f failingAgent) GetReplica(context.Context, agent.GetReplicaRequest) (agent.GetReplicaResponse, error) {
	return agent.GetReplicaResponse{}, f.err
}
