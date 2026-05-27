package spdk

import (
	"context"
	"strings"
	"testing"

	"github.com/bouquet2/kdfs/internal/names"
)

func TestAttachNVMeControllerParamsUseControllerAdrFam(t *testing.T) {
	params := attachNVMeControllerParams(NVMeController{
		Name:    "nvme0",
		TrAddr:  "2001:db8::10",
		TrSvcID: "4421",
		SubNQN:  "nqn.2026-05.krea.to:replica-pvc-1234-1",
		AdrFam:  "ipv6",
	})

	if got := params["adrfam"]; got != "ipv6" {
		t.Fatalf("adrfam = %v", got)
	}
}

func TestHostNQNPattern(t *testing.T) {
	validHosts := []string{
		"nqn.2026-05.krea.to:krea.to:host-worker-1",
	}
	for _, host := range validHosts {
		if !names.IsHostNQN(host) {
			t.Fatalf("expected valid host NQN: %q", host)
		}
	}

	invalidHosts := []string{
		"worker-1",
		"krea.to:host-worker-1",
		"nqn.2026-05.krea.to:krea.to",
		"nqn.2026-05.krea.to:krea.to:host-worker 1",
		"nqn.2026-05.example.com:storage.example.com:host-node-a",
	}
	for _, host := range invalidHosts {
		if names.IsHostNQN(host) {
			t.Fatalf("expected invalid host NQN: %q", host)
		}
	}
}

func TestAddHostRejectsInvalidHostNQN(t *testing.T) {
	client := &realClient{}
	err := client.AddHost(context.Background(), "nqn.2026-05.krea.to:volume-demo", "worker-1")
	if err == nil {
		t.Fatal("expected invalid host NQN to be rejected")
	}
	if !strings.Contains(err.Error(), "invalid host NQN") {
		t.Fatalf("unexpected error: %v", err)
	}
}
