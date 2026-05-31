package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSnapshotDefaults(t *testing.T) {
	snap := &Snapshot{
		ObjectMeta: metav1.ObjectMeta{Name: "snap-abc", Namespace: "kdfs"},
		Spec:       SnapshotSpec{VolumeRef: "pvc-1234", SnapshotID: "snap-abc"},
	}
	if snap.Spec.VolumeRef != "pvc-1234" {
		t.Fatal("wrong volumeRef")
	}
	if snap.Spec.SnapshotID != "snap-abc" {
		t.Fatal("wrong snapshotID")
	}
	if snap.Status.Phase != "" {
		t.Fatal("expected empty phase")
	}
}

func TestSnapshotPhaseConstants(t *testing.T) {
	if SnapshotPhasePending != "Pending" {
		t.Fatal()
	}
	if SnapshotPhaseReady != "Ready" {
		t.Fatal()
	}
	if SnapshotPhaseFailed != "Failed" {
		t.Fatal()
	}
}

func TestSnapshotSourceInVolumeSpec(t *testing.T) {
	v := Volume{ObjectMeta: metav1.ObjectMeta{Name: "restored-vol"}, Spec: VolumeSpec{
		Size: "10Gi", NodeID: "worker-1",
		SnapshotSource: &SnapshotSource{SnapshotName: "snap-abc"},
	}}
	if v.Spec.SnapshotSource == nil {
		t.Fatal("SnapshotSource should be set")
	}
	if v.Spec.SnapshotSource.SnapshotName != "snap-abc" {
		t.Fatal()
	}
}
