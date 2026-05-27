package sidecar

import (
	"context"
	"reflect"
	"testing"

	"github.com/bouquet2/kdfs/internal/network"
	"github.com/bouquet2/kdfs/internal/spdk"
)

func TestConfigureReplicaExportsAIOBdev(t *testing.T) {
	fake := &spdk.FakeClient{}
	result, err := ConfigureReplica(context.Background(), fake, ReplicaConfig{
		VolumeName: "pvc-1234",
		DataPath:   "/data/pvc-1234/vol.img",
		Listener:   spdk.Listener{TrType: "TCP", AdrFam: "IPv4", TrAddr: "10.0.0.5", TrSvcID: "4421"},
		Endpoint:   "10.0.0.5:4421",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.NQN != "nqn.2026-05.krea.to:replica-pvc-1234-0" || result.Endpoint != "10.0.0.5:4421" {
		t.Fatalf("result = %#v", result)
	}
	want := []string{"CreateAIOBdev:aio0:/data/pvc-1234/vol.img", "CreateTransport:tcp", "CreateSubsystem:nqn.2026-05.krea.to:replica-pvc-1234-0", "AddNamespace:aio0", "AddListener:4421:IPv4:10.0.0.5"}
	if !reflect.DeepEqual(fake.Calls, want) {
		t.Fatalf("calls = %#v", fake.Calls)
	}
}

func TestConfigureReplicaReturnsCanonicalIPv6Endpoint(t *testing.T) {
	fake := &spdk.FakeClient{}
	result, err := ConfigureReplica(context.Background(), fake, ReplicaConfig{
		VolumeName: "pvc-1234",
		DataPath:   "/data/pvc-1234/vol.img",
		Listener:   spdk.Listener{TrType: "TCP", AdrFam: "IPv6", TrAddr: "2001:db8::10", TrSvcID: "4421"},
		Endpoint:   network.JoinEndpoint("2001:db8::10", "4421"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Endpoint != "[2001:db8::10]:4421" {
		t.Fatalf("endpoint = %q", result.Endpoint)
	}
}

func TestReplicaStatusReturnsCanonicalEndpoint(t *testing.T) {
	replica := &Replica{result: ReplicaResult{
		NQN:      "nqn.2026-05.krea.to:replica-pvc-1234-0",
		Endpoint: "[2001:db8::10]:4421",
	}}

	status := replica.Status()
	if status.Role != "replica" || status.Endpoint != "[2001:db8::10]:4421" || status.ReplicaNQN != "nqn.2026-05.krea.to:replica-pvc-1234-0" {
		t.Fatalf("status = %#v", status)
	}
}
