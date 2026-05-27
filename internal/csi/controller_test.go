//go:build linux

package csi

import (
	"context"
	"testing"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	"github.com/bouquet2/kdfs/internal/names"
	csipb "github.com/container-storage-interface/spec/lib/go/csi"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func csiTestClient(t *testing.T, objects ...runtime.Object) *Driver {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := storagev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return &Driver{Client: fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).Build(), Namespace: "kdfs", NodeID: "worker-1"}
}

func csiCreateVolumeReq(name, nodeID string, caps ...csipb.VolumeCapability_AccessMode_Mode) *csipb.CreateVolumeRequest {
	req := &csipb.CreateVolumeRequest{Name: name, CapacityRange: &csipb.CapacityRange{RequiredBytes: 10737418240}, Parameters: map[string]string{"storageClassName": "kdfs"}}
	if nodeID != "" {
		req.AccessibilityRequirements = &csipb.TopologyRequirement{
			Requisite: []*csipb.Topology{{Segments: map[string]string{"topology.storage.krea.to/hostname": nodeID}}},
		}
	}
	for _, m := range caps {
		req.VolumeCapabilities = append(req.VolumeCapabilities, &csipb.VolumeCapability{AccessMode: &csipb.VolumeCapability_AccessMode{Mode: m}})
	}
	return req
}

func TestCreateVolumeCreatesVolumeCR(t *testing.T) {
	driver := csiTestClient(t)
	resp, err := driver.CreateVolume(context.Background(), csiCreateVolumeReq("pvc-1234", ""))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Volume.VolumeId != "pvc-1234" {
		t.Fatalf("response = %#v", resp)
	}
	volume := &storagev1alpha1.Volume{}
	if err := driver.Client.Get(context.Background(), types.NamespacedName{Name: "pvc-1234", Namespace: "kdfs"}, volume); err != nil {
		t.Fatal(err)
	}
	if volume.Spec.NodeID != "worker-1" || volume.Spec.Size != "10737418240" {
		t.Fatalf("volume spec = %#v", volume.Spec)
	}
}

func TestCreateVolumePinsTopologyForSingleNode(t *testing.T) {
	driver := csiTestClient(t)
	resp, err := driver.CreateVolume(context.Background(), csiCreateVolumeReq("pvc-single", "worker-2", csipb.VolumeCapability_AccessMode_SINGLE_NODE_WRITER))
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Volume.AccessibleTopology) != 1 || resp.Volume.AccessibleTopology[0].Segments["topology.storage.krea.to/hostname"] != "worker-2" {
		t.Fatalf("expected pinned topology for single-node volume, got %#v", resp.Volume.AccessibleTopology)
	}
}

func TestCreateVolumeOmitsTopologyForROX(t *testing.T) {
	driver := csiTestClient(t)
	resp, err := driver.CreateVolume(context.Background(), csiCreateVolumeReq("pvc-rox", "worker-2", csipb.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY))
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Volume.AccessibleTopology) != 0 {
		t.Fatalf("expected empty topology for ROX volume, got %#v", resp.Volume.AccessibleTopology)
	}
}

func TestCreateVolumeOmitsTopologyForRWX(t *testing.T) {
	driver := csiTestClient(t)
	resp, err := driver.CreateVolume(context.Background(), csiCreateVolumeReq("pvc-rwx", "worker-2", csipb.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER))
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Volume.AccessibleTopology) != 0 {
		t.Fatalf("expected empty topology for RWX volume, got %#v", resp.Volume.AccessibleTopology)
	}
}

func TestControllerPublishVolumeReturnsEngineEndpoint(t *testing.T) {
	engine := &storagev1alpha1.Engine{ObjectMeta: metav1.ObjectMeta{Name: names.EngineName("pvc-1234"), Namespace: "kdfs"}, Status: storagev1alpha1.EngineStatus{Phase: storagev1alpha1.EnginePhaseRunning, Endpoint: "worker-1:4420", SubsystemNQN: names.VolumeNQN("pvc-1234")}}
	driver := csiTestClient(t, engine)
	resp, err := driver.ControllerPublishVolume(context.Background(), &csipb.ControllerPublishVolumeRequest{
		VolumeId: "pvc-1234", NodeId: "worker-1",
		VolumeCapability: &csipb.VolumeCapability{AccessMode: &csipb.VolumeCapability_AccessMode{Mode: csipb.VolumeCapability_AccessMode_SINGLE_NODE_WRITER}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.PublishContext["endpoint"] != "worker-1:4420" || resp.PublishContext["nqn"] != names.VolumeNQN("pvc-1234") {
		t.Fatalf("publish context = %#v", resp.PublishContext)
	}
}

func TestControllerPublishVolumeROX(t *testing.T) {
	engine := &storagev1alpha1.Engine{ObjectMeta: metav1.ObjectMeta{Name: names.EngineName("pvc-1234"), Namespace: "kdfs"}, Status: storagev1alpha1.EngineStatus{Phase: storagev1alpha1.EnginePhaseRunning, Endpoint: "worker-1:4420", SubsystemNQN: names.VolumeNQN("pvc-1234")}}
	driver := csiTestClient(t, engine)
	resp, err := driver.ControllerPublishVolume(context.Background(), &csipb.ControllerPublishVolumeRequest{
		VolumeId: "pvc-1234", NodeId: "worker-2",
		VolumeCapability: &csipb.VolumeCapability{AccessMode: &csipb.VolumeCapability_AccessMode{Mode: csipb.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.PublishContext["endpoint"] != "worker-1:4420" || resp.PublishContext["nqn"] != names.VolumeNQN("pvc-1234") {
		t.Fatalf("rox publish context = %#v", resp.PublishContext)
	}
}

func TestControllerPublishVolumeRWX(t *testing.T) {
	engine := &storagev1alpha1.Engine{ObjectMeta: metav1.ObjectMeta{Name: names.EngineName("pvc-1234"), Namespace: "kdfs"}, Status: storagev1alpha1.EngineStatus{Phase: storagev1alpha1.EnginePhaseRunning, Endpoint: "10.0.0.5:4420", SubsystemNQN: names.VolumeNQN("pvc-1234")}}
	driver := csiTestClient(t, engine)
	resp, err := driver.ControllerPublishVolume(context.Background(), &csipb.ControllerPublishVolumeRequest{
		VolumeId: "pvc-1234", NodeId: "worker-2",
		VolumeCapability: &csipb.VolumeCapability{AccessMode: &csipb.VolumeCapability_AccessMode{Mode: csipb.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.PublishContext["protocol"] != "nfs" || resp.PublishContext["server"] != "10.0.0.5" || resp.PublishContext["export"] != "/" {
		t.Fatalf("rwx publish context = %#v", resp.PublishContext)
	}
}

func TestControllerPublishVolumeRWXExtractsIPv6ServerHost(t *testing.T) {
	engine := &storagev1alpha1.Engine{ObjectMeta: metav1.ObjectMeta{Name: names.EngineName("pvc-1234"), Namespace: "kdfs"}, Status: storagev1alpha1.EngineStatus{Phase: storagev1alpha1.EnginePhaseRunning, Endpoint: "[fd00::50]:4420", SubsystemNQN: names.VolumeNQN("pvc-1234")}}
	driver := csiTestClient(t, engine)
	resp, err := driver.ControllerPublishVolume(context.Background(), &csipb.ControllerPublishVolumeRequest{
		VolumeId: "pvc-1234", NodeId: "worker-2",
		VolumeCapability: &csipb.VolumeCapability{AccessMode: &csipb.VolumeCapability_AccessMode{Mode: csipb.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.PublishContext["protocol"] != "nfs" || resp.PublishContext["server"] != "fd00::50" || resp.PublishContext["export"] != "/" {
		t.Fatalf("rwx publish context = %#v", resp.PublishContext)
	}
}
