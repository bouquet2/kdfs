package controller

import (
	"context"
	"testing"
	"time"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	"github.com/bouquet2/kdfs/internal/names"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestVolumeReconcileCreatesEngineAndReplicas(t *testing.T) {
	ctx := context.Background()
	volume := newTestVolume("pvc-1234", "kdfs", "worker-1")
	node1 := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-1"}}
	node2 := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-2"}}
	c := newFakeClient(t, volume, node1, node2).WithStatusSubresource(&storagev1alpha1.Volume{}).Build()

	r := &VolumeReconciler{Client: c, Scheme: testScheme(t)}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: volume.Name, Namespace: volume.Namespace}})
	if err != nil {
		t.Fatal(err)
	}

	engine := &storagev1alpha1.Engine{}
	if err := c.Get(ctx, types.NamespacedName{Name: names.EngineName(volume.Name), Namespace: volume.Namespace}, engine); err != nil {
		t.Fatalf("engine not created: %v", err)
	}
	if engine.Spec.NodeID != "worker-1" {
		t.Fatalf("engine node = %q", engine.Spec.NodeID)
	}
	if len(engine.Spec.Replicas) != 2 {
		t.Fatalf("expected 2 replicas, got %d", len(engine.Spec.Replicas))
	}

	replica0 := &storagev1alpha1.Replica{}
	if err := c.Get(ctx, types.NamespacedName{Name: names.ReplicaName(volume.Name, 0), Namespace: volume.Namespace}, replica0); err != nil {
		t.Fatalf("replica-0 not created: %v", err)
	}
	if replica0.Spec.Type != storagev1alpha1.ReplicaTypeRemote || replica0.Spec.NodeID != "worker-1" {
		t.Fatalf("replica-0 spec = %#v", replica0.Spec)
	}

	replica1 := &storagev1alpha1.Replica{}
	if err := c.Get(ctx, types.NamespacedName{Name: names.ReplicaName(volume.Name, 1), Namespace: volume.Namespace}, replica1); err != nil {
		t.Fatalf("replica-1 not created: %v", err)
	}
	if replica1.Spec.Type != storagev1alpha1.ReplicaTypeRemote || replica1.Spec.NodeID != "worker-2" {
		t.Fatalf("replica-1 spec = %#v", replica1.Spec)
	}
}

func TestVolumeReconcileMarksReadyWhenChildrenRunning(t *testing.T) {
	ctx := context.Background()
	volume := newTestVolume("pvc-1234", "kdfs", "worker-1")
	volume.Status.EngineRef = &storagev1alpha1.NamespacedObjectReference{Name: names.EngineName(volume.Name), Namespace: volume.Namespace}
	volume.Status.Phase = storagev1alpha1.VolumePhaseCreating
	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: names.EngineName(volume.Name), Namespace: volume.Namespace},
		Spec: storagev1alpha1.EngineSpec{
			NodeID: "worker-1",
			Replicas: []storagev1alpha1.ReplicaAttachment{
				{Name: names.ReplicaName(volume.Name, 0), NodeID: "worker-1"},
				{Name: names.ReplicaName(volume.Name, 1), NodeID: "worker-2"},
			},
		},
		Status: storagev1alpha1.EngineStatus{Phase: storagev1alpha1.EnginePhaseRunning},
	}
	replica0 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName(volume.Name, 0), Namespace: volume.Namespace}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning}}
	replica1 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName(volume.Name, 1), Namespace: volume.Namespace}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning}}
	c := newFakeClient(t, volume, engine, replica0, replica1).WithStatusSubresource(&storagev1alpha1.Volume{}).Build()

	r := &VolumeReconciler{Client: c, Scheme: testScheme(t)}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: volume.Name, Namespace: volume.Namespace}})
	if err != nil {
		t.Fatal(err)
	}

	updated := &storagev1alpha1.Volume{}
	if err := c.Get(ctx, types.NamespacedName{Name: volume.Name, Namespace: volume.Namespace}, updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status.Phase != storagev1alpha1.VolumePhaseReady {
		t.Fatalf("volume phase = %q", updated.Status.Phase)
	}
}

func TestVolumeReconcileIgnoresDeletedVolume(t *testing.T) {
	ctx := context.Background()
	c := newFakeClient(t).Build()
	r := &VolumeReconciler{Client: c, Scheme: testScheme(t)}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "missing", Namespace: "kdfs"}})
	if err != nil {
		t.Fatal(err)
	}
	engine := &storagev1alpha1.Engine{}
	err = c.Get(ctx, types.NamespacedName{Name: names.EngineName("missing"), Namespace: "kdfs"}, engine)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestVolumeReconcilePicksNodeWhenNodeIDEmpty(t *testing.T) {
	ctx := context.Background()
	volume := newTestVolume("pvc-1234", "kdfs", "")
	cp := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "kdfs-control-plane", Labels: map[string]string{"node-role.kubernetes.io/control-plane": ""}}}
	worker1 := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-1"}}
	worker2 := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-2"}}
	c := newFakeClient(t, volume, cp, worker1, worker2).WithStatusSubresource(&storagev1alpha1.Volume{}).Build()

	r := &VolumeReconciler{Client: c, Scheme: testScheme(t)}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: volume.Name, Namespace: volume.Namespace}})
	if err != nil {
		t.Fatal(err)
	}

	engine := &storagev1alpha1.Engine{}
	if err := c.Get(ctx, types.NamespacedName{Name: names.EngineName(volume.Name), Namespace: volume.Namespace}, engine); err != nil {
		t.Fatalf("engine not created: %v", err)
	}
	if engine.Spec.NodeID == "kdfs-control-plane" {
		t.Fatalf("engine node = %q, should not be control-plane", engine.Spec.NodeID)
	}
	if len(engine.Spec.Replicas) < 2 {
		t.Fatalf("expected at least 2 replicas, got %d", len(engine.Spec.Replicas))
	}
	for _, rep := range engine.Spec.Replicas {
		if rep.NodeID == "kdfs-control-plane" {
			t.Fatalf("replica on control-plane: %#v", rep)
		}
	}
}

func TestScaleUp(t *testing.T) {
	scheme := runtime.NewScheme()
	storagev1alpha1.AddToScheme(scheme)
	corev1.AddToScheme(scheme)
	k8sfake := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&storagev1alpha1.Volume{}).Build()
	r := &VolumeReconciler{Client: k8sfake, Scheme: scheme}

	vol := &storagev1alpha1.Volume{
		ObjectMeta: metav1.ObjectMeta{Name: "pvc-123", Namespace: "default"},
		Spec:       storagev1alpha1.VolumeSpec{Size: "1Gi", NodeID: "worker1", ReplicaCount: "3"},
	}
	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: "pvc-123-engine", Namespace: "default"},
		Spec: storagev1alpha1.EngineSpec{
			VolumeRef: storagev1alpha1.LocalObjectReference{Name: "pvc-123"},
			NodeID:    "worker1",
			Replicas: []storagev1alpha1.ReplicaAttachment{
				{Name: "pvc-123-replica-0", NodeID: "worker1"},
				{Name: "pvc-123-replica-1", NodeID: "worker2"},
			},
		},
	}
	vol.Status.EngineRef = &storagev1alpha1.NamespacedObjectReference{Name: engine.Name, Namespace: engine.Namespace}
	k8sfake.Create(context.Background(), vol)
	k8sfake.Create(context.Background(), engine)
	k8sfake.Create(context.Background(), &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker1"}})
	k8sfake.Create(context.Background(), &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker2"}})
	k8sfake.Create(context.Background(), &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker3"}})

	err := r.ensureScale(context.Background(), vol, engine)
	assert.NoError(t, err)

	var replica storagev1alpha1.Replica
	err = k8sfake.Get(context.Background(), types.NamespacedName{Name: "pvc-123-replica-2", Namespace: "default"}, &replica)
	assert.NoError(t, err)
	assert.Equal(t, storagev1alpha1.ReplicaTypeRemote, replica.Spec.Type)
	assert.Equal(t, "worker3", replica.Spec.NodeID)

	var updatedEngine storagev1alpha1.Engine
	k8sfake.Get(context.Background(), types.NamespacedName{Name: engine.Name, Namespace: engine.Namespace}, &updatedEngine)
	assert.Len(t, updatedEngine.Spec.Replicas, 3)
	assert.Equal(t, "pvc-123-replica-2", updatedEngine.Spec.Replicas[2].Name)
	assert.Equal(t, storagev1alpha1.VolumePhaseDegraded, vol.Status.Phase)
}

func findHealthEntry(t *testing.T, healths []storagev1alpha1.ReplicaHealth, name string) storagev1alpha1.ReplicaHealth {
	t.Helper()
	for _, h := range healths {
		if h.Name == name {
			return h
		}
	}
	t.Fatalf("health entry for %s not found", name)
	return storagev1alpha1.ReplicaHealth{}
}

func TestHealReplicas_BelowThreshold_StaysDegraded(t *testing.T) {
	ctx := context.Background()
	volume := newTestVolume("pvc-test", "kdfs", "worker-1")
	volume.Status.EngineRef = &storagev1alpha1.NamespacedObjectReference{Name: names.EngineName(volume.Name), Namespace: volume.Namespace}
	volume.Spec.ReplicaCount = "3"

	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: names.EngineName(volume.Name), Namespace: volume.Namespace},
		Spec: storagev1alpha1.EngineSpec{
			NodeID: "worker-1",
			Replicas: []storagev1alpha1.ReplicaAttachment{
				{Name: names.ReplicaName(volume.Name, 0), NodeID: "worker-1"},
				{Name: names.ReplicaName(volume.Name, 1), NodeID: "worker-2"},
				{Name: names.ReplicaName(volume.Name, 2), NodeID: "worker-3"},
			},
		},
	}
	replica0 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName(volume.Name, 0), Namespace: volume.Namespace}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: "nqn.0"}}
	replica1 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName(volume.Name, 1), Namespace: volume.Namespace}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseFailed}}
	replica2 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName(volume.Name, 2), Namespace: volume.Namespace}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseFailed}}
	pod0 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName(volume.Name, 0) + "-pod", Namespace: volume.Namespace, Labels: map[string]string{"kdfs.krea.to/mode": "replica"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.1"}}

	c := newFakeClient(t, volume, engine, replica0, replica1, replica2, pod0).WithStatusSubresource(&storagev1alpha1.Volume{}).Build()
	r := &VolumeReconciler{Client: c, Scheme: testScheme(t)}

	healed, _, err := r.healReplicas(ctx, volume, engine)
	assert.NoError(t, err)
	assert.False(t, healed)

	updated := &storagev1alpha1.Volume{}
	c.Get(ctx, client.ObjectKeyFromObject(volume), updated)
	assert.Equal(t, storagev1alpha1.VolumePhaseDegraded, updated.Status.Phase)
}

func TestHealReplicas_MissingReplica_RemovesAttachment(t *testing.T) {
	ctx := context.Background()
	volume := newTestVolume("pvc-test", "kdfs", "worker-1")
	volume.Status.EngineRef = &storagev1alpha1.NamespacedObjectReference{Name: names.EngineName(volume.Name), Namespace: volume.Namespace}
	volume.Spec.ReplicaCount = "2"

	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: names.EngineName(volume.Name), Namespace: volume.Namespace},
		Spec: storagev1alpha1.EngineSpec{
			NodeID: "worker-1",
			Replicas: []storagev1alpha1.ReplicaAttachment{
				{Name: names.ReplicaName(volume.Name, 0), NodeID: "worker-1"},
				{Name: names.ReplicaName(volume.Name, 1), NodeID: "worker-2"},
			},
		},
	}
	replica0 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName(volume.Name, 0), Namespace: volume.Namespace}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: "nqn.0"}}
	pod0 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName(volume.Name, 0) + "-pod", Namespace: volume.Namespace, Labels: map[string]string{"kdfs.krea.to/mode": "replica"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.1"}}

	c := newFakeClient(t, volume, engine, replica0, pod0).WithStatusSubresource(&storagev1alpha1.Volume{}).Build()
	r := &VolumeReconciler{Client: c, Scheme: testScheme(t)}

	healed, _, err := r.healReplicas(ctx, volume, engine)
	assert.NoError(t, err)
	assert.True(t, healed)

	assert.Len(t, engine.Spec.Replicas, 1)
	assert.Equal(t, names.ReplicaName(volume.Name, 0), engine.Spec.Replicas[0].Name)
}

func TestHealReplicas_PodRestart_DeletesPod(t *testing.T) {
	ctx := context.Background()
	volume := newTestVolume("pvc-test", "kdfs", "worker-1")
	volume.Status.EngineRef = &storagev1alpha1.NamespacedObjectReference{Name: names.EngineName(volume.Name), Namespace: volume.Namespace}
	volume.Spec.ReplicaCount = "2"

	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: names.EngineName(volume.Name), Namespace: volume.Namespace},
		Spec: storagev1alpha1.EngineSpec{
			NodeID: "worker-1",
			Replicas: []storagev1alpha1.ReplicaAttachment{
				{Name: names.ReplicaName(volume.Name, 0), NodeID: "worker-1"},
				{Name: names.ReplicaName(volume.Name, 1), NodeID: "worker-2"},
			},
		},
	}
	replica0 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName(volume.Name, 0), Namespace: volume.Namespace}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: "nqn.0"}}
	replica1 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName(volume.Name, 1), Namespace: volume.Namespace}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: ""}}
	pod0 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName(volume.Name, 0) + "-pod", Namespace: volume.Namespace, Labels: map[string]string{"kdfs.krea.to/mode": "replica"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.1"}}
	pod1 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName(volume.Name, 1) + "-pod", Namespace: volume.Namespace, Labels: map[string]string{"kdfs.krea.to/mode": "replica"}}, Status: corev1.PodStatus{Phase: corev1.PodPending, PodIP: "10.0.0.2"}}

	c := newFakeClient(t, volume, engine, replica0, replica1, pod0, pod1).WithStatusSubresource(&storagev1alpha1.Volume{}).Build()
	r := &VolumeReconciler{Client: c, Scheme: testScheme(t)}

	healed, _, err := r.healReplicas(ctx, volume, engine)
	assert.NoError(t, err)
	assert.True(t, healed)

	err = c.Get(ctx, client.ObjectKeyFromObject(pod1), &corev1.Pod{})
	assert.True(t, apierrors.IsNotFound(err))

	updated := &storagev1alpha1.Volume{}
	c.Get(ctx, client.ObjectKeyFromObject(volume), updated)
	assert.Equal(t, 1, findHealthEntry(t, updated.Status.ReplicaHealth, names.ReplicaName(volume.Name, 1)).RestartAttempts)
}

func TestHealReplicas_ExhaustedRetries_DeletesReplica(t *testing.T) {
	ctx := context.Background()
	volume := newTestVolume("pvc-test", "kdfs", "worker-1")
	volume.Status.EngineRef = &storagev1alpha1.NamespacedObjectReference{Name: names.EngineName(volume.Name), Namespace: volume.Namespace}
	volume.Spec.ReplicaCount = "2"
	replName := names.ReplicaName(volume.Name, 1)
	volume.Status.ReplicaHealth = []storagev1alpha1.ReplicaHealth{
		{Name: replName, NodeID: "worker-2", Phase: "Pending", RestartAttempts: 5},
	}

	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: names.EngineName(volume.Name), Namespace: volume.Namespace},
		Spec: storagev1alpha1.EngineSpec{
			NodeID: "worker-1",
			Replicas: []storagev1alpha1.ReplicaAttachment{
				{Name: names.ReplicaName(volume.Name, 0), NodeID: "worker-1"},
				{Name: replName, NodeID: "worker-2"},
			},
		},
	}
	replica0 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName(volume.Name, 0), Namespace: volume.Namespace}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: "nqn.0"}}
	replica1 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: replName, Namespace: volume.Namespace}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: ""}}
	pod0 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName(volume.Name, 0) + "-pod", Namespace: volume.Namespace, Labels: map[string]string{"kdfs.krea.to/mode": "replica"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.1"}}
	pod1 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: replName + "-pod", Namespace: volume.Namespace, Labels: map[string]string{"kdfs.krea.to/mode": "replica"}}, Status: corev1.PodStatus{Phase: corev1.PodPending, PodIP: "10.0.0.2"}}

	c := newFakeClient(t, volume, engine, replica0, replica1, pod0, pod1).WithStatusSubresource(&storagev1alpha1.Volume{}).Build()
	r := &VolumeReconciler{Client: c, Scheme: testScheme(t)}

	healed, _, err := r.healReplicas(ctx, volume, engine)
	assert.NoError(t, err)
	assert.True(t, healed)

	err = c.Get(ctx, client.ObjectKeyFromObject(replica1), &storagev1alpha1.Replica{})
	assert.True(t, apierrors.IsNotFound(err))

	assert.Len(t, engine.Spec.Replicas, 1)
}

func TestHealReplicas_BackoffCooldown_SkipsRestart(t *testing.T) {
	ctx := context.Background()
	volume := newTestVolume("pvc-test", "kdfs", "worker-1")
	volume.Status.EngineRef = &storagev1alpha1.NamespacedObjectReference{Name: names.EngineName(volume.Name), Namespace: volume.Namespace}
	volume.Spec.ReplicaCount = "2"
	replName := names.ReplicaName(volume.Name, 1)
	twoSecondsAgo := metav1.NewTime(time.Now().Add(-2 * time.Second))
	volume.Status.ReplicaHealth = []storagev1alpha1.ReplicaHealth{
		{Name: replName, NodeID: "worker-2", Phase: "Pending", RestartAttempts: 1, LastHealTime: &twoSecondsAgo},
	}

	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: names.EngineName(volume.Name), Namespace: volume.Namespace},
		Spec: storagev1alpha1.EngineSpec{
			NodeID: "worker-1",
			Replicas: []storagev1alpha1.ReplicaAttachment{
				{Name: names.ReplicaName(volume.Name, 0), NodeID: "worker-1"},
				{Name: replName, NodeID: "worker-2"},
			},
		},
	}
	replica0 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName(volume.Name, 0), Namespace: volume.Namespace}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: "nqn.0"}}
	replica1 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: replName, Namespace: volume.Namespace}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: ""}}
	pod0 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName(volume.Name, 0) + "-pod", Namespace: volume.Namespace, Labels: map[string]string{"kdfs.krea.to/mode": "replica"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.1"}}
	pod1 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: replName + "-pod", Namespace: volume.Namespace, Labels: map[string]string{"kdfs.krea.to/mode": "replica"}}, Status: corev1.PodStatus{Phase: corev1.PodPending, PodIP: "10.0.0.2"}}

	c := newFakeClient(t, volume, engine, replica0, replica1, pod0, pod1).WithStatusSubresource(&storagev1alpha1.Volume{}).Build()
	r := &VolumeReconciler{Client: c, Scheme: testScheme(t)}

	healed, requeueAfter, err := r.healReplicas(ctx, volume, engine)
	assert.NoError(t, err)
	assert.False(t, healed)
	assert.Greater(t, requeueAfter, time.Duration(0))
}

func TestScaleDown(t *testing.T) {
	scheme := runtime.NewScheme()
	storagev1alpha1.AddToScheme(scheme)
	corev1.AddToScheme(scheme)
	k8sfake := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&storagev1alpha1.Volume{}).Build()
	r := &VolumeReconciler{Client: k8sfake, Scheme: scheme}

	vol := &storagev1alpha1.Volume{
		ObjectMeta: metav1.ObjectMeta{Name: "pvc-456", Namespace: "default"},
		Spec:       storagev1alpha1.VolumeSpec{Size: "1Gi", NodeID: "worker1", ReplicaCount: "1"},
	}
	rep1 := &storagev1alpha1.Replica{
		ObjectMeta: metav1.ObjectMeta{Name: "pvc-456-replica-1", Namespace: "default"},
		Spec:       storagev1alpha1.ReplicaSpec{VolumeRef: storagev1alpha1.LocalObjectReference{Name: "pvc-456"}},
	}
	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: "pvc-456-engine", Namespace: "default"},
		Spec: storagev1alpha1.EngineSpec{
			VolumeRef: storagev1alpha1.LocalObjectReference{Name: "pvc-456"},
			NodeID:    "worker1",
			Replicas: []storagev1alpha1.ReplicaAttachment{
				{Name: "pvc-456-replica-0", NodeID: "worker1"},
				{Name: "pvc-456-replica-1", NodeID: "worker2"},
			},
		},
	}
	vol.Status.EngineRef = &storagev1alpha1.NamespacedObjectReference{Name: engine.Name, Namespace: engine.Namespace}
	k8sfake.Create(context.Background(), vol)
	k8sfake.Create(context.Background(), engine)
	k8sfake.Create(context.Background(), rep1)

	err := r.ensureScale(context.Background(), vol, engine)
	assert.NoError(t, err)

	var replica1 storagev1alpha1.Replica
	err = k8sfake.Get(context.Background(), types.NamespacedName{Name: "pvc-456-replica-1", Namespace: "default"}, &replica1)
	assert.True(t, apierrors.IsNotFound(err))

	var updatedEngine storagev1alpha1.Engine
	k8sfake.Get(context.Background(), types.NamespacedName{Name: engine.Name, Namespace: engine.Namespace}, &updatedEngine)
	assert.Len(t, updatedEngine.Spec.Replicas, 1)
	assert.Equal(t, storagev1alpha1.VolumePhaseDegraded, vol.Status.Phase)
}

func TestReplicasForVolumeRejectsZeroReplicaCountString(t *testing.T) {
	ctx := context.Background()
	volume := newTestVolume("pvc-1234", "kdfs", "worker-1")
	volume.Spec.ReplicaCount = "0"
	worker1 := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-1"}}
	c := newFakeClient(t, volume, worker1).Build()

	r := &VolumeReconciler{Client: c, Scheme: testScheme(t)}
	_, err := r.replicasForVolume(ctx, volume)
	if err == nil {
		t.Fatal("expected error for zero replica count")
	}
	if err.Error() != "invalid replicaCount \"0\": must be \"auto\" or a positive integer" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReplicasForVolumeUsesExplicitReplicaCountString(t *testing.T) {
	ctx := context.Background()
	volume := newTestVolume("pvc-1234", "kdfs", "worker-1")
	volume.Spec.ReplicaCount = "3"
	worker1 := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-1"}}
	c := newFakeClient(t, volume, worker1).Build()

	r := &VolumeReconciler{Client: c, Scheme: testScheme(t)}
	count, err := r.replicasForVolume(ctx, volume)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("count = %d", count)
	}
}
