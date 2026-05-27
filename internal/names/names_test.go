package names

import "testing"

func TestResourceNames(t *testing.T) {
	volume := "pvc-1234"
	if got := EngineName(volume); got != "pvc-1234-engine" {
		t.Fatalf("EngineName() = %q", got)
	}
	if got := ReplicaName(volume, 0); got != "pvc-1234-replica-0" {
		t.Fatalf("ReplicaName(0) = %q", got)
	}
	if got := ReplicaName(volume, 1); got != "pvc-1234-replica-1" {
		t.Fatalf("ReplicaName(1) = %q", got)
	}
}

func TestNQNs(t *testing.T) {
	if got := VolumeNQN("pvc-1234"); got != "nqn.2026-05.krea.to:volume-pvc-1234" {
		t.Fatalf("VolumeNQN() = %q", got)
	}
	if got := ReplicaNQN("pvc-1234", 0); got != "nqn.2026-05.krea.to:replica-pvc-1234-0" {
		t.Fatalf("ReplicaNQN(0) = %q", got)
	}
	if got := HostNQN("worker-1"); got != "nqn.2026-05.krea.to:krea.to:host-worker-1" {
		t.Fatalf("HostNQN() = %q", got)
	}
	if !IsHostNQN("nqn.2026-05.krea.to:krea.to:host-worker-1") {
		t.Fatal("expected valid host NQN")
	}
	if IsHostNQN("nqn.2026-05.example.com:storage.example.com:host-worker-1") {
		t.Fatal("expected different authority to be rejected")
	}
}

func TestNQNsUseDefaultAuthorityWhenEnvUnset(t *testing.T) {
	t.Setenv("NQN_AUTHORITY", "")
	if got := VolumeNQN("pvc-1234"); got != "nqn.2026-05.krea.to:volume-pvc-1234" {
		t.Fatalf("VolumeNQN() = %q", got)
	}
	if got := HostNQN("worker-1"); got != "nqn.2026-05.krea.to:krea.to:host-worker-1" {
		t.Fatalf("HostNQN() = %q", got)
	}
}

func TestNQNsUseConfiguredAuthority(t *testing.T) {
	t.Setenv("NQN_AUTHORITY", "nqn.2026-05.cluster-b.example")
	if got := VolumeNQN("pvc-1234"); got != "nqn.2026-05.cluster-b.example:volume-pvc-1234" {
		t.Fatalf("VolumeNQN() = %q", got)
	}
	if got := HostNQN("worker-1"); got != "nqn.2026-05.cluster-b.example:krea.to:host-worker-1" {
		t.Fatalf("HostNQN() = %q", got)
	}
}

func TestIsHostNQNUseConfiguredAuthority(t *testing.T) {
	t.Setenv("NQN_AUTHORITY", "nqn.2026-05.cluster-b.example")
	if !IsHostNQN("nqn.2026-05.cluster-b.example:krea.to:host-worker-1") {
		t.Fatal("expected configured authority host NQN to be valid")
	}
	if IsHostNQN("nqn.2026-05.krea.to:krea.to:host-worker-1") {
		t.Fatal("expected foreign authority host NQN to be rejected")
	}
}

func TestDataPath(t *testing.T) {
	if got := DataPath("pvc-1234"); got != "/var/lib/kdfs/pvc-1234/vol.img" {
		t.Fatalf("DataPath() = %q", got)
	}
}
