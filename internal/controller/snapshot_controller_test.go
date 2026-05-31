package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	"github.com/bouquet2/kdfs/internal/agent"
	"github.com/bouquet2/kdfs/internal/names"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestSnapshotReconcile_CreatesPending(t *testing.T) {
	ctx := context.Background()
	snap := &storagev1alpha1.Snapshot{
		ObjectMeta: metav1.ObjectMeta{Name: "snap-abc", Namespace: "kdfs"},
		Spec:       storagev1alpha1.SnapshotSpec{VolumeRef: "pvc-1234", SnapshotID: "snap-abc"},
	}
	c := newFakeClient(t, snap).WithStatusSubresource(&storagev1alpha1.Snapshot{}).Build()
	r := &SnapshotReconciler{Client: c, Scheme: testScheme(t)}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: snap.Name, Namespace: snap.Namespace}})
	if err != nil {
		t.Fatal(err)
	}

	updated := &storagev1alpha1.Snapshot{}
	c.Get(ctx, types.NamespacedName{Name: snap.Name, Namespace: snap.Namespace}, updated)
	if !containsString(updated.Finalizers, SnapshotFinalizer) {
		t.Fatal("expected finalizer to be added")
	}

	_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: snap.Name, Namespace: snap.Namespace}})
	if err != nil {
		t.Fatal(err)
	}

	c.Get(ctx, types.NamespacedName{Name: snap.Name, Namespace: snap.Namespace}, updated)
	if updated.Status.Phase != storagev1alpha1.SnapshotPhasePending {
		t.Fatalf("expected Pending, got %s", updated.Status.Phase)
	}
}

func TestSnapshotReconcile_TransitionsToCreating(t *testing.T) {
	ctx := context.Background()
	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: names.EngineName("pvc-1234"), Namespace: "kdfs"},
		Spec:       storagev1alpha1.EngineSpec{NodeID: "worker-1"},
	}
	snap := &storagev1alpha1.Snapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: "snap-abc", Namespace: "kdfs",
			Finalizers: []string{SnapshotFinalizer},
		},
		Spec: storagev1alpha1.SnapshotSpec{VolumeRef: "pvc-1234", SnapshotID: "snap-abc"},
		Status: storagev1alpha1.SnapshotStatus{Phase: storagev1alpha1.SnapshotPhasePending},
	}
	c := newFakeClient(t, snap, engine).WithStatusSubresource(&storagev1alpha1.Snapshot{}).Build()
	r := &SnapshotReconciler{Client: c, Scheme: testScheme(t)}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: snap.Name, Namespace: snap.Namespace}})
	if err != nil {
		t.Fatal(err)
	}

	updated := &storagev1alpha1.Snapshot{}
	c.Get(ctx, types.NamespacedName{Name: snap.Name, Namespace: snap.Namespace}, updated)
	if updated.Status.Phase != storagev1alpha1.SnapshotPhaseCreating {
		t.Fatalf("expected Creating, got %s", updated.Status.Phase)
	}
	if updated.Status.EngineNode != "worker-1" {
		t.Fatal("wrong engineNode")
	}
}

func TestSnapshotReconcile_CompletesSnapshot(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"snapshotPath": "/var/lib/kdfs/pvc-1234/snapshot-snap-abc.img",
			"sizeBytes":    10737418240,
		})
	}))
	defer srv.Close()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pvc-1234-engine-pod", Namespace: "kdfs"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.244.1.10"},
	}
	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: names.EngineName("pvc-1234"), Namespace: "kdfs"},
		Spec:       storagev1alpha1.EngineSpec{NodeID: "worker-1"},
		Status:     storagev1alpha1.EngineStatus{PodRef: &storagev1alpha1.PodReference{Name: pod.Name, Namespace: pod.Namespace}},
	}
	snap := &storagev1alpha1.Snapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: "snap-abc", Namespace: "kdfs",
			Finalizers: []string{SnapshotFinalizer},
		},
		Spec:   storagev1alpha1.SnapshotSpec{VolumeRef: "pvc-1234", SnapshotID: "snap-abc"},
		Status: storagev1alpha1.SnapshotStatus{Phase: storagev1alpha1.SnapshotPhaseCreating, EngineNode: "worker-1"},
	}
	c := newFakeClient(t, snap, engine, pod).WithStatusSubresource(&storagev1alpha1.Snapshot{}, &storagev1alpha1.Engine{}).Build()
	r := &SnapshotReconciler{
		Client: c, Scheme: testScheme(t),
		SnapshotURLFn: func(podIP string) string { return srv.URL + "/snapshot" },
	}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: snap.Name, Namespace: snap.Namespace}})
	if err != nil {
		t.Fatal(err)
	}

	updated := &storagev1alpha1.Snapshot{}
	c.Get(ctx, types.NamespacedName{Name: snap.Name, Namespace: snap.Namespace}, updated)
	if updated.Status.Phase != storagev1alpha1.SnapshotPhaseReady {
		t.Fatalf("expected Ready, got %s", updated.Status.Phase)
	}
	if !updated.Status.ReadyToUse {
		t.Fatal("expected ReadyToUse")
	}
	if updated.Status.SizeBytes != 10737418240 {
		t.Fatal("wrong size")
	}
}

func TestSnapshotReconcile_DeletesViaNodeAgent(t *testing.T) {
	ctx := context.Background()
	var deletedPath string
	agentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req agent.DeleteSnapshotRequest
		json.NewDecoder(r.Body).Decode(&req)
		deletedPath = req.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer agentSrv.Close()

	snap := &storagev1alpha1.Snapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: "snap-abc", Namespace: "kdfs",
			Finalizers:        []string{SnapshotFinalizer},
			DeletionTimestamp: &metav1.Time{Time: time.Now()},
		},
		Spec: storagev1alpha1.SnapshotSpec{VolumeRef: "pvc-1234", SnapshotID: "snap-abc"},
		Status: storagev1alpha1.SnapshotStatus{
			Phase:        storagev1alpha1.SnapshotPhaseReady,
			SnapshotPath: "/var/lib/kdfs/pvc-1234/snapshot-snap-abc.img",
			EngineNode:   "worker-1",
		},
	}
	c := newFakeClient(t, snap).WithStatusSubresource(&storagev1alpha1.Snapshot{}).Build()
	r := &SnapshotReconciler{
		Client: c, Scheme: testScheme(t),
		AgentFactory: func(_ context.Context, _ string) (agent.Client, error) {
			return agent.NewHTTPClient(agentSrv.URL, 5*time.Second), nil
		},
	}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: snap.Name, Namespace: snap.Namespace}})
	if err != nil {
		t.Fatal(err)
	}
	if deletedPath != "/var/lib/kdfs/pvc-1234/snapshot-snap-abc.img" {
		t.Fatalf("wrong deleted path: %q", deletedPath)
	}

	updated := &storagev1alpha1.Snapshot{}
	c.Get(ctx, types.NamespacedName{Name: snap.Name, Namespace: snap.Namespace}, updated)
	if containsString(updated.Finalizers, SnapshotFinalizer) {
		t.Fatal("expected finalizer to be removed")
	}
}
