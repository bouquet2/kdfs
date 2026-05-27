package controller

import (
	"context"
	"errors"
	"net/http"
	"testing"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	"github.com/bouquet2/kdfs/internal/names"
	"github.com/bouquet2/kdfs/internal/sidecar"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

func replicaPod(name, namespace, ip string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: map[string]string{"kdfs.krea.to/mode": "replica"}},
		Status:     corev1.PodStatus{PodIP: ip},
	}
}

func TestEngineReconcileCreatesPodWhenReplicasReady(t *testing.T) {
	ctx := context.Background()
	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: names.EngineName("pvc-1234"), Namespace: "kdfs"},
		Spec: storagev1alpha1.EngineSpec{
			VolumeRef: storagev1alpha1.LocalObjectReference{Name: "pvc-1234"},
			NodeID:    "worker-1",
			Replicas: []storagev1alpha1.ReplicaAttachment{
				{Name: names.ReplicaName("pvc-1234", 0), NodeID: "worker-1"},
				{Name: names.ReplicaName("pvc-1234", 1), NodeID: "worker-2"},
			},
		},
	}
	replica0 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 0), Namespace: "kdfs"}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: names.ReplicaNQN("pvc-1234", 0), Endpoint: "worker-1:4421"}}
	replica1 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 1), Namespace: "kdfs"}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: names.ReplicaNQN("pvc-1234", 1), Endpoint: "worker-2:4421"}}
	rpod0 := replicaPod(names.ReplicaName("pvc-1234", 0)+"-pod", "kdfs", "10.244.1.50")
	rpod1 := replicaPod(names.ReplicaName("pvc-1234", 1)+"-pod", "kdfs", "10.244.1.99")
	c := newFakeClient(t, engine, replica0, replica1, rpod0, rpod1).WithStatusSubresource(&storagev1alpha1.Engine{}).Build()
	r := &EngineReconciler{Client: c, Scheme: testScheme(t)}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: engine.Name, Namespace: engine.Namespace}})
	if err != nil {
		t.Fatal(err)
	}
	updated := &storagev1alpha1.Engine{}
	if err := c.Get(ctx, types.NamespacedName{Name: engine.Name, Namespace: engine.Namespace}, updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status.PodRef == nil || updated.Status.Phase != storagev1alpha1.EnginePhasePending {
		t.Fatalf("status = %#v", updated.Status)
	}
	pod := &corev1.Pod{}
	if err := c.Get(ctx, types.NamespacedName{Name: names.EngineName("pvc-1234") + "-pod", Namespace: "kdfs"}, pod); err != nil {
		t.Fatal(err)
	}
	if len(pod.OwnerReferences) != 1 {
		t.Fatalf("owner refs = %#v", pod.OwnerReferences)
	}
	ref := pod.OwnerReferences[0]
	if ref.Controller == nil || !*ref.Controller {
		t.Fatalf("expected controller owner reference, got %#v", ref)
	}
}

func TestEngineReconcileKeepsPendingUntilPodReady(t *testing.T) {
	ctx := context.Background()
	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: names.EngineName("pvc-1234"), Namespace: "kdfs"},
		Spec: storagev1alpha1.EngineSpec{
			VolumeRef: storagev1alpha1.LocalObjectReference{Name: "pvc-1234"},
			NodeID:    "worker-1",
			Replicas: []storagev1alpha1.ReplicaAttachment{
				{Name: names.ReplicaName("pvc-1234", 0), NodeID: "worker-1"},
				{Name: names.ReplicaName("pvc-1234", 1), NodeID: "worker-2"},
			},
		},
	}
	engine.Status.PodRef = &storagev1alpha1.PodReference{Name: names.EngineName("pvc-1234") + "-pod", Namespace: "kdfs"}
	engine.Status.Phase = storagev1alpha1.EnginePhasePending
	replica0 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 0), Namespace: "kdfs"}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: names.ReplicaNQN("pvc-1234", 0), Endpoint: "worker-1:4421", BdevName: "bdev0"}}
	replica1 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 1), Namespace: "kdfs"}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: names.ReplicaNQN("pvc-1234", 1), Endpoint: "worker-2:4421", BdevName: "bdev1"}}
	rpod0 := replicaPod(names.ReplicaName("pvc-1234", 0)+"-pod", "kdfs", "10.244.1.50")
	rpod1 := replicaPod(names.ReplicaName("pvc-1234", 1)+"-pod", "kdfs", "10.244.1.99")
	epod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: names.EngineName("pvc-1234") + "-pod", Namespace: "kdfs"},
		Status: corev1.PodStatus{
			PodIP:             "10.244.1.50",
			ContainerStatuses: []corev1.ContainerStatus{{Name: "sidecar", Ready: false}},
		},
	}
	c := newFakeClient(t, engine, replica0, replica1, rpod0, rpod1, epod).WithStatusSubresource(&storagev1alpha1.Engine{}).Build()
	r := &EngineReconciler{Client: c, Scheme: testScheme(t)}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: engine.Name, Namespace: engine.Namespace}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Requeue {
		t.Fatalf("expected requeue while engine pod is not ready, got %#v", result)
	}
	updated := &storagev1alpha1.Engine{}
	if err := c.Get(ctx, types.NamespacedName{Name: engine.Name, Namespace: engine.Namespace}, updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status.Phase != storagev1alpha1.EnginePhasePending {
		t.Fatalf("expected phase to remain pending, got %#v", updated.Status)
	}
}

func TestEngineReconcileMarksRunningWhenReplicasRunning(t *testing.T) {
	ctx := context.Background()
	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: names.EngineName("pvc-1234"), Namespace: "kdfs"},
		Spec: storagev1alpha1.EngineSpec{
			VolumeRef: storagev1alpha1.LocalObjectReference{Name: "pvc-1234"},
			NodeID:    "worker-1",
			Replicas: []storagev1alpha1.ReplicaAttachment{
				{Name: names.ReplicaName("pvc-1234", 0), NodeID: "worker-1"},
				{Name: names.ReplicaName("pvc-1234", 1), NodeID: "worker-2"},
			},
		},
	}
	engine.Status.PodRef = &storagev1alpha1.PodReference{Name: names.EngineName("pvc-1234") + "-pod", Namespace: "kdfs"}
	engine.Status.Phase = storagev1alpha1.EnginePhasePending
	replica0 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 0), Namespace: "kdfs"}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: names.ReplicaNQN("pvc-1234", 0), Endpoint: "worker-1:4421", BdevName: "bdev0"}}
	replica1 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 1), Namespace: "kdfs"}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: names.ReplicaNQN("pvc-1234", 1), Endpoint: "worker-2:4421", BdevName: "bdev1"}}
	rpod0 := replicaPod(names.ReplicaName("pvc-1234", 0)+"-pod", "kdfs", "10.244.1.50")
	rpod1 := replicaPod(names.ReplicaName("pvc-1234", 1)+"-pod", "kdfs", "10.244.1.99")
	epod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: names.EngineName("pvc-1234") + "-pod", Namespace: "kdfs"}, Status: corev1.PodStatus{PodIP: "10.244.1.50", ContainerStatuses: []corev1.ContainerStatus{{Name: "spdk-engine", Ready: true}, {Name: "sidecar", Ready: true}, {Name: "nfs", Ready: true}}}}
	c := newFakeClient(t, engine, replica0, replica1, rpod0, rpod1, epod).WithStatusSubresource(&storagev1alpha1.Engine{}).Build()
	r := &EngineReconciler{Client: c, Scheme: testScheme(t), SidecarStatusClient: &fakeSidecarStatusClient{status: sidecar.Status{Role: "engine", Endpoint: "worker-1:4420", SubsystemNQN: names.VolumeNQN("pvc-1234")}}}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: engine.Name, Namespace: engine.Namespace}})
	if err != nil {
		t.Fatal(err)
	}
	updated := &storagev1alpha1.Engine{}
	if err := c.Get(ctx, types.NamespacedName{Name: engine.Name, Namespace: engine.Namespace}, updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status.Phase != storagev1alpha1.EnginePhaseRunning || updated.Status.SubsystemNQN != names.VolumeNQN("pvc-1234") {
		t.Fatalf("status = %#v", updated.Status)
	}
	if updated.Status.ROXEndpoint != "" || updated.Status.ROXSubsystemNQN != "" {
		t.Fatalf("expected empty ROX fields in single-subsystem mode, got ROXEndpoint=%q ROXSubsystemNQN=%q", updated.Status.ROXEndpoint, updated.Status.ROXSubsystemNQN)
	}
}

func TestEngineReconcileUsesReplicaEndpointHostNotPodIP(t *testing.T) {
	ctx := context.Background()
	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: names.EngineName("pvc-1234"), Namespace: "kdfs"},
		Spec: storagev1alpha1.EngineSpec{
			VolumeRef: storagev1alpha1.LocalObjectReference{Name: "pvc-1234"},
			NodeID:    "worker-1",
			Replicas:  []storagev1alpha1.ReplicaAttachment{{Name: names.ReplicaName("pvc-1234", 0), NodeID: "worker-1"}, {Name: names.ReplicaName("pvc-1234", 1), NodeID: "worker-2"}},
		},
	}
	replica0 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 0), Namespace: "kdfs"}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: names.ReplicaNQN("pvc-1234", 0), Endpoint: "worker-1:4421"}}
	replica1 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 1), Namespace: "kdfs"}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: names.ReplicaNQN("pvc-1234", 1), Endpoint: "[fd00::21]:4421"}}
	rpod0 := replicaPod(names.ReplicaName("pvc-1234", 0)+"-pod", "kdfs", "10.244.1.50")
	rpod1 := replicaPod(names.ReplicaName("pvc-1234", 1)+"-pod", "kdfs", "10.244.1.99")
	c := newFakeClient(t, engine, replica0, replica1, rpod0, rpod1).Build()
	r := &EngineReconciler{Client: c, Scheme: testScheme(t)}

	ready, err := r.replicaStatus(ctx, engine)
	if err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Fatal("expected replicas to be ready")
	}
	if engine.Spec.Replicas[1].Address != "fd00::21" || engine.Spec.Replicas[1].Port != "4421" {
		t.Fatalf("replica attachment = %#v", engine.Spec.Replicas[1])
	}
	if engine.Spec.Replicas[1].Address == rpod1.Status.PodIP {
		t.Fatalf("address should not use pod IP: %#v", engine.Spec.Replicas[1])
	}
}

func TestEngineReconcilePublishesEndpointFromSidecarStatus(t *testing.T) {
	ctx := context.Background()
	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: names.EngineName("pvc-1234"), Namespace: "kdfs"},
		Spec: storagev1alpha1.EngineSpec{
			VolumeRef: storagev1alpha1.LocalObjectReference{Name: "pvc-1234"},
			NodeID:    "worker-1",
			Replicas:  []storagev1alpha1.ReplicaAttachment{{Name: names.ReplicaName("pvc-1234", 0), NodeID: "worker-1"}},
		},
		Status: storagev1alpha1.EngineStatus{PodRef: &storagev1alpha1.PodReference{Name: names.EngineName("pvc-1234") + "-pod", Namespace: "kdfs"}, Phase: storagev1alpha1.EnginePhasePending},
	}
	replica0 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 0), Namespace: "kdfs"}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: names.ReplicaNQN("pvc-1234", 0), Endpoint: "worker-1:4421", BdevName: "bdev0"}}
	rpod0 := replicaPod(names.ReplicaName("pvc-1234", 0)+"-pod", "kdfs", "10.244.1.50")
	epod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: names.EngineName("pvc-1234") + "-pod", Namespace: "kdfs"}, Status: corev1.PodStatus{PodIP: "fd00::50", ContainerStatuses: []corev1.ContainerStatus{{Name: "spdk-engine", Ready: true}, {Name: "sidecar", Ready: true}, {Name: "nfs", Ready: true}}}}
	c := newFakeClient(t, engine, replica0, rpod0, epod).WithStatusSubresource(&storagev1alpha1.Engine{}).Build()
	statusClient := &fakeSidecarStatusClient{status: sidecar.Status{Role: "engine", Endpoint: "[fd00::60]:4420", SubsystemNQN: "nqn.2014-08.org.nvmexpress:pvc-1234"}}
	r := &EngineReconciler{Client: c, Scheme: testScheme(t), SidecarStatusClient: statusClient}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: engine.Name, Namespace: engine.Namespace}})
	if err != nil {
		t.Fatal(err)
	}

	updated := &storagev1alpha1.Engine{}
	if err := c.Get(ctx, types.NamespacedName{Name: engine.Name, Namespace: engine.Namespace}, updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status.Endpoint != "[fd00::60]:4420" {
		t.Fatalf("endpoint = %q", updated.Status.Endpoint)
	}
	if updated.Status.SubsystemNQN != "nqn.2014-08.org.nvmexpress:pvc-1234" {
		t.Fatalf("subsystem nqn = %q", updated.Status.SubsystemNQN)
	}
	if len(statusClient.podIPs) != 1 || statusClient.podIPs[0] != "fd00::50" {
		t.Fatalf("sidecar podIPs = %#v", statusClient.podIPs)
	}
}

func TestEngineReconcileRequeuesOnMalformedSidecarStatus(t *testing.T) {
	ctx := context.Background()
	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: names.EngineName("pvc-1234"), Namespace: "kdfs"},
		Spec: storagev1alpha1.EngineSpec{
			VolumeRef: storagev1alpha1.LocalObjectReference{Name: "pvc-1234"},
			NodeID:    "worker-1",
			Replicas:  []storagev1alpha1.ReplicaAttachment{{Name: names.ReplicaName("pvc-1234", 0), NodeID: "worker-1"}},
		},
		Status: storagev1alpha1.EngineStatus{PodRef: &storagev1alpha1.PodReference{Name: names.EngineName("pvc-1234") + "-pod", Namespace: "kdfs"}, Phase: storagev1alpha1.EnginePhasePending},
	}
	replica0 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 0), Namespace: "kdfs"}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: names.ReplicaNQN("pvc-1234", 0), Endpoint: "worker-1:4421", BdevName: "bdev0"}}
	rpod0 := replicaPod(names.ReplicaName("pvc-1234", 0)+"-pod", "kdfs", "10.244.1.50")
	epod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: names.EngineName("pvc-1234") + "-pod", Namespace: "kdfs"}, Status: corev1.PodStatus{PodIP: "fd00::50", ContainerStatuses: []corev1.ContainerStatus{{Name: "spdk-engine", Ready: true}, {Name: "sidecar", Ready: true}, {Name: "nfs", Ready: true}}}}
	c := newFakeClient(t, engine, replica0, rpod0, epod).WithStatusSubresource(&storagev1alpha1.Engine{}).Build()
	r := &EngineReconciler{Client: c, Scheme: testScheme(t), SidecarStatusClient: &fakeSidecarStatusClient{status: sidecar.Status{Role: "replica", Endpoint: "[fd00::60]:4420", SubsystemNQN: names.VolumeNQN("pvc-1234")}}}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: engine.Name, Namespace: engine.Namespace}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Requeue {
		t.Fatalf("result = %#v, want requeue", result)
	}
}

func TestEngineReconcileRequeuesOnMalformedSidecarEndpoint(t *testing.T) {
	ctx := context.Background()
	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: names.EngineName("pvc-1234"), Namespace: "kdfs"},
		Spec: storagev1alpha1.EngineSpec{
			VolumeRef: storagev1alpha1.LocalObjectReference{Name: "pvc-1234"},
			NodeID:    "worker-1",
			Replicas:  []storagev1alpha1.ReplicaAttachment{{Name: names.ReplicaName("pvc-1234", 0), NodeID: "worker-1"}},
		},
		Status: storagev1alpha1.EngineStatus{PodRef: &storagev1alpha1.PodReference{Name: names.EngineName("pvc-1234") + "-pod", Namespace: "kdfs"}, Phase: storagev1alpha1.EnginePhasePending},
	}
	replica0 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 0), Namespace: "kdfs"}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: names.ReplicaNQN("pvc-1234", 0), Endpoint: "worker-1:4421", BdevName: "bdev0"}}
	rpod0 := replicaPod(names.ReplicaName("pvc-1234", 0)+"-pod", "kdfs", "10.244.1.50")
	epod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: names.EngineName("pvc-1234") + "-pod", Namespace: "kdfs"}, Status: corev1.PodStatus{PodIP: "fd00::50", ContainerStatuses: []corev1.ContainerStatus{{Name: "spdk-engine", Ready: true}, {Name: "sidecar", Ready: true}, {Name: "nfs", Ready: true}}}}
	c := newFakeClient(t, engine, replica0, rpod0, epod).WithStatusSubresource(&storagev1alpha1.Engine{}).Build()
	r := &EngineReconciler{Client: c, Scheme: testScheme(t), SidecarStatusClient: &fakeSidecarStatusClient{status: sidecar.Status{Role: "engine", Endpoint: ":4420", SubsystemNQN: names.VolumeNQN("pvc-1234")}}}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: engine.Name, Namespace: engine.Namespace}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Requeue {
		t.Fatalf("result = %#v, want requeue", result)
	}
}

func TestEngineReconcileRequeuesOnEmptyPortSidecarEndpoint(t *testing.T) {
	ctx := context.Background()
	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: names.EngineName("pvc-1234"), Namespace: "kdfs"},
		Spec: storagev1alpha1.EngineSpec{
			VolumeRef: storagev1alpha1.LocalObjectReference{Name: "pvc-1234"},
			NodeID:    "worker-1",
			Replicas:  []storagev1alpha1.ReplicaAttachment{{Name: names.ReplicaName("pvc-1234", 0), NodeID: "worker-1"}},
		},
		Status: storagev1alpha1.EngineStatus{PodRef: &storagev1alpha1.PodReference{Name: names.EngineName("pvc-1234") + "-pod", Namespace: "kdfs"}, Phase: storagev1alpha1.EnginePhasePending},
	}
	replica0 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 0), Namespace: "kdfs"}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: names.ReplicaNQN("pvc-1234", 0), Endpoint: "worker-1:4421", BdevName: "bdev0"}}
	rpod0 := replicaPod(names.ReplicaName("pvc-1234", 0)+"-pod", "kdfs", "10.244.1.50")
	epod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: names.EngineName("pvc-1234") + "-pod", Namespace: "kdfs"}, Status: corev1.PodStatus{PodIP: "fd00::50", ContainerStatuses: []corev1.ContainerStatus{{Name: "spdk-engine", Ready: true}, {Name: "sidecar", Ready: true}, {Name: "nfs", Ready: true}}}}
	c := newFakeClient(t, engine, replica0, rpod0, epod).WithStatusSubresource(&storagev1alpha1.Engine{}).Build()
	r := &EngineReconciler{Client: c, Scheme: testScheme(t), SidecarStatusClient: &fakeSidecarStatusClient{status: sidecar.Status{Role: "engine", Endpoint: "worker-1:", SubsystemNQN: names.VolumeNQN("pvc-1234")}}}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: engine.Name, Namespace: engine.Namespace}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Requeue {
		t.Fatalf("result = %#v, want requeue", result)
	}
}

func TestReconfigureURLBracketsIPv6PodIP(t *testing.T) {
	if got := reconfigureURL("fd00::50"); got != "http://[fd00::50]:9810/reconfigure" {
		t.Fatalf("url = %q", got)
	}
}

func TestReconfigureTriggered(t *testing.T) {
	prev := newReconfigureHTTPClient
	defer func() { newReconfigureHTTPClient = prev }()
	newReconfigureHTTPClient = func() *http.Client {
		return &http.Client{Transport: reconfigureRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != "http://10.0.0.5:9810/reconfigure" {
				t.Fatalf("url = %q", req.URL.String())
			}
			return nil, errors.New("boom")
		})}
	}

	ctx := context.Background()
	volName := "pvc-reconfig"
	engineName := names.EngineName(volName)

	replica0 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName(volName, 0), Namespace: "default"}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: names.ReplicaNQN(volName, 0), Endpoint: "worker1:4421", BdevName: "bdev0"}}
	replica1 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName(volName, 1), Namespace: "default"}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: names.ReplicaNQN(volName, 1), Endpoint: "worker2:4421", BdevName: "bdev1"}}
	rpod0 := replicaPod(names.ReplicaName(volName, 0)+"-pod", "default", "10.0.0.1")
	rpod1 := replicaPod(names.ReplicaName(volName, 1)+"-pod", "default", "10.0.0.2")
	epod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: engineName + "-pod", Namespace: "default"}, Status: corev1.PodStatus{PodIP: "10.0.0.5", ContainerStatuses: []corev1.ContainerStatus{{Name: "spdk-engine", Ready: true}, {Name: "sidecar", Ready: true}, {Name: "nfs", Ready: true}}}}

	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: engineName, Namespace: "default"},
		Spec: storagev1alpha1.EngineSpec{
			VolumeRef: storagev1alpha1.LocalObjectReference{Name: volName},
			NodeID:    "worker1",
			Replicas: []storagev1alpha1.ReplicaAttachment{
				{Name: names.ReplicaName(volName, 0), NodeID: "worker1"},
				{Name: names.ReplicaName(volName, 1), NodeID: "worker2"},
			},
		},
		Status: storagev1alpha1.EngineStatus{
			PodRef:           &storagev1alpha1.PodReference{Name: engineName + "-pod", Namespace: "default"},
			LastReplicasHash: "oldhash",
		},
	}
	c := newFakeClient(t, engine, replica0, replica1, rpod0, rpod1, epod).WithStatusSubresource(&storagev1alpha1.Engine{}).Build()
	r := &EngineReconciler{Client: c, Scheme: testScheme(t), SidecarStatusClient: &fakeSidecarStatusClient{status: sidecar.Status{Role: "engine", Endpoint: "worker1:4420", SubsystemNQN: names.VolumeNQN(volName)}}}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: engine.Name, Namespace: engine.Namespace}})
	assert.NoError(t, err)

	updated := &storagev1alpha1.Engine{}
	c.Get(ctx, types.NamespacedName{Name: engine.Name, Namespace: engine.Namespace}, updated)
	assert.Equal(t, storagev1alpha1.EnginePhaseDegraded, updated.Status.Phase)
}

type reconfigureRoundTripFunc func(*http.Request) (*http.Response, error)

func (f reconfigureRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestAllowedHostsIncludesEngineNode(t *testing.T) {
	ctx := context.Background()
	worker1 := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-1"}}
	worker2 := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-2"}}
	cp := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "cp-1", Labels: map[string]string{"node-role.kubernetes.io/control-plane": ""}}}
	c := newFakeClient(t, worker1, worker2, cp).Build()

	r := &EngineReconciler{Client: c, Scheme: testScheme(t)}
	hosts := r.allowedHosts(ctx, "worker-1")
	want := []string{names.HostNQN("worker-1"), names.HostNQN("worker-2")}
	assert.Equal(t, want, hosts)
}

func TestEngineReconcileRecreatesMissingPodOnNextReconcile(t *testing.T) {
	ctx := context.Background()
	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: names.EngineName("pvc-1234"), Namespace: "kdfs"},
		Spec: storagev1alpha1.EngineSpec{
			VolumeRef: storagev1alpha1.LocalObjectReference{Name: "pvc-1234"},
			NodeID:    "worker-1",
			Replicas: []storagev1alpha1.ReplicaAttachment{
				{Name: names.ReplicaName("pvc-1234", 0), NodeID: "worker-1"},
				{Name: names.ReplicaName("pvc-1234", 1), NodeID: "worker-2"},
			},
		},
		Status: storagev1alpha1.EngineStatus{
			PodRef: &storagev1alpha1.PodReference{Name: names.EngineName("pvc-1234") + "-pod", Namespace: "kdfs"},
			Phase:  storagev1alpha1.EnginePhaseRunning,
		},
	}
	replica0 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 0), Namespace: "kdfs"}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: names.ReplicaNQN("pvc-1234", 0), Endpoint: "worker-1:4421"}}
	replica1 := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: names.ReplicaName("pvc-1234", 1), Namespace: "kdfs"}, Status: storagev1alpha1.ReplicaStatus{Phase: storagev1alpha1.ReplicaPhaseRunning, NQN: names.ReplicaNQN("pvc-1234", 1), Endpoint: "worker-2:4421"}}
	rpod0 := replicaPod(names.ReplicaName("pvc-1234", 0)+"-pod", "kdfs", "10.244.1.50")
	rpod1 := replicaPod(names.ReplicaName("pvc-1234", 1)+"-pod", "kdfs", "10.244.1.99")
	worker1 := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-1"}}
	worker2 := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-2"}}
	c := newFakeClient(t, engine, replica0, replica1, rpod0, rpod1, worker1, worker2).WithStatusSubresource(&storagev1alpha1.Engine{}).Build()
	r := &EngineReconciler{Client: c, Scheme: testScheme(t)}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: engine.Name, Namespace: engine.Namespace}})
	if err != nil {
		t.Fatal(err)
	}

	updated := &storagev1alpha1.Engine{}
	if err := c.Get(ctx, types.NamespacedName{Name: engine.Name, Namespace: engine.Namespace}, updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status.PodRef != nil {
		t.Fatalf("expected stale podRef to be cleared, got %#v", updated.Status.PodRef)
	}

	_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: engine.Name, Namespace: engine.Namespace}})
	if err != nil {
		t.Fatal(err)
	}

	if err := c.Get(ctx, types.NamespacedName{Name: names.EngineName("pvc-1234") + "-pod", Namespace: "kdfs"}, &corev1.Pod{}); err != nil {
		t.Fatalf("expected engine pod to be recreated: %v", err)
	}
}
