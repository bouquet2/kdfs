package sidecar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	"github.com/bouquet2/kdfs/internal/network"
	"github.com/bouquet2/kdfs/internal/spdk"
)

func TestConfigureEngineCreatesExpectedSPDKStack(t *testing.T) {
	fake := &spdk.FakeClient{}
	config := EngineConfig{
		VolumeName: "pvc-1234",
		LocalPath:  "/data/pvc-1234/vol.img",
		Replicas: []storagev1alpha1.ReplicaAttachment{
			{Name: "pvc-1234-replica-1", NodeID: "worker-2", NQN: "nqn.2026-05.krea.to:replica-pvc-1234-1", Address: "10.0.0.6", Port: "4421"},
		},
		Listener: spdk.Listener{TrType: "TCP", AdrFam: "IPv4", TrAddr: "10.0.0.5", TrSvcID: "4420"},
		Endpoint: "10.0.0.5:4420",
	}
	engine, err := ConfigureEngine(context.Background(), fake, config)
	if err != nil {
		t.Fatal(err)
	}
	if engine.volumeName != "pvc-1234" {
		t.Fatalf("volumeName = %q, want pvc-1234", engine.volumeName)
	}
	if engine.subsystemNQN != "nqn.2026-05.krea.to:volume-pvc-1234" {
		t.Fatalf("subsystemNQN = %q", engine.subsystemNQN)
	}
	want := []string{"CreateTransport:tcp", "CreateAIOBdev:aio0:/data/pvc-1234/vol.img", "AttachNVMeController:nvme0:nqn.2026-05.krea.to:replica-pvc-1234-1:ipv4:10.0.0.6", "CreateMirrorBdev:raid0", "CreateSubsystem:nqn.2026-05.krea.to:volume-pvc-1234", "AddNamespace:raid0", "AddListener:4420:IPv4:10.0.0.5"}
	if !reflect.DeepEqual(fake.Calls, want) {
		t.Fatalf("calls = %#v", fake.Calls)
	}
	if got := engine.Endpoint(); got != "10.0.0.5:4420" {
		t.Fatalf("Endpoint() = %q", got)
	}
}

func TestConfigureEngineSkipsLocalReplica(t *testing.T) {
	fake := &spdk.FakeClient{}
	config := EngineConfig{
		VolumeName: "pvc-1234",
		LocalPath:  "/data/pvc-1234/vol.img",
		Replicas: []storagev1alpha1.ReplicaAttachment{
			{Name: "pvc-1234-replica-0", NodeID: "worker-1", IsLocal: true, NQN: "nqn.2026-05.krea.to:replica-pvc-1234-0", Address: "10.0.0.5", Port: "4421"},
			{Name: "pvc-1234-replica-1", NodeID: "worker-2", IsLocal: false, NQN: "nqn.2026-05.krea.to:replica-pvc-1234-1", Address: "10.0.0.6", Port: "4421"},
		},
		Listener: spdk.Listener{TrType: "TCP", AdrFam: "IPv4", TrAddr: "10.0.0.5", TrSvcID: "4420"},
		Endpoint: "10.0.0.5:4420",
	}
	engine, err := ConfigureEngine(context.Background(), fake, config)
	if err != nil {
		t.Fatal(err)
	}
	if engine.volumeName != "pvc-1234" {
		t.Fatalf("volumeName = %q, want pvc-1234", engine.volumeName)
	}
	want := []string{"CreateTransport:tcp", "CreateAIOBdev:aio0:/data/pvc-1234/vol.img", "AttachNVMeController:nvme1:nqn.2026-05.krea.to:replica-pvc-1234-1:ipv4:10.0.0.6", "CreateMirrorBdev:raid0", "CreateSubsystem:nqn.2026-05.krea.to:volume-pvc-1234", "AddNamespace:raid0", "AddListener:4420:IPv4:10.0.0.5"}
	if !reflect.DeepEqual(fake.Calls, want) {
		t.Fatalf("calls = %#v\nwant   = %#v", fake.Calls, want)
	}
}

func TestConfigureEngineUsesResolvedIPv6Listener(t *testing.T) {
	fake := &spdk.FakeClient{}
	config := EngineConfig{
		VolumeName: "pvc-1234",
		LocalPath:  "/data/pvc-1234/vol.img",
		Replicas: []storagev1alpha1.ReplicaAttachment{
			{Name: "pvc-1234-replica-1", NodeID: "worker-2", NQN: "nqn.2026-05.krea.to:replica-pvc-1234-1", Address: "2001:db8::20", Port: "4421"},
		},
		Listener: spdk.Listener{TrType: "TCP", AdrFam: "IPv6", TrAddr: "2001:db8::10", TrSvcID: "4420"},
		Endpoint: network.JoinEndpoint("2001:db8::10", "4420"),
	}

	engine, err := ConfigureEngine(context.Background(), fake, config)
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.Endpoint(); got != "[2001:db8::10]:4420" {
		t.Fatalf("Endpoint() = %q", got)
	}
	if got := fake.Calls[len(fake.Calls)-1]; got != "AddListener:4420:IPv6:2001:db8::10" {
		t.Fatalf("last call = %q", got)
	}
	if got := fake.Calls[2]; got != "AttachNVMeController:nvme0:nqn.2026-05.krea.to:replica-pvc-1234-1:ipv6:2001:db8::20" {
		t.Fatalf("attach call = %q", got)
	}
}

func TestEngineStatusHTTPReturnsEndpointStatus(t *testing.T) {
	engine := NewEngine(&spdk.FakeClient{}, EngineConfig{
		VolumeName: "pvc-1234",
		Listener:   spdk.Listener{TrType: "TCP", AdrFam: "IPv6", TrAddr: "2001:db8::10", TrSvcID: "4420"},
		Endpoint:   "[2001:db8::10]:4420",
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	engine.StatusHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q", got)
	}
	var status Status
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Role != "engine" || status.Endpoint != "[2001:db8::10]:4420" || status.SubsystemNQN != "nqn.2026-05.krea.to:volume-pvc-1234" {
		t.Fatalf("status = %#v", status)
	}
}
