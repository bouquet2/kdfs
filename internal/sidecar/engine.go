package sidecar

import (
	"context"
	"fmt"
	"strings"
	"time"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	"github.com/bouquet2/kdfs/internal/logging"
	"github.com/bouquet2/kdfs/internal/names"
	"github.com/bouquet2/kdfs/internal/network"
	"github.com/bouquet2/kdfs/internal/spdk"
)

var engineLogger = logging.Component("sidecar-engine")

type EngineConfig struct {
	VolumeName   string
	LocalPath    string
	Replicas     []storagev1alpha1.ReplicaAttachment
	Listener     spdk.Listener
	Endpoint     string
	AllowedHosts []string
}

func nvmeAddressFamily(address string) (string, error) {
	family, err := network.AddressFamilyForAddress(address, network.FamilyAuto)
	if err != nil {
		return "ipv4", nil
	}
	return strings.ToLower(family), nil
}

// Builds the SPDK stack for a volume, attaching remote replicas and exporting the target subsystem.
func ConfigureEngine(ctx context.Context, client spdk.Client, config EngineConfig) (*Engine, error) {
	for _, step := range []func() error{
		func() error { return client.CreateTransport(ctx, "tcp") },
		func() error { return client.CreateAIOBdev(ctx, "aio0", config.LocalPath, 4096) },
	} {
		if err := step(); err != nil {
			return nil, err
		}
	}
	baseBdevs := []string{"aio0"}

	for i, rep := range config.Replicas {
		if rep.IsLocal {
			engineLogger.Info().Int("replica_index", i).Str("replica", rep.Name).Msg("replica is local on same node, skipping NVMe-oF attach")
			continue
		}
		if rep.Address == "" || rep.NQN == "" {
			engineLogger.Warn().Int("replica_index", i).Str("replica", rep.Name).Msg("replica has no endpoint info, skipping")
			continue
		}
		adrFam, err := nvmeAddressFamily(rep.Address)
		if err != nil {
			return nil, fmt.Errorf("resolve replica address family: %w", err)
		}
		bdevName := fmt.Sprintf("nvme%d", i)
		for attempt := range 5 {
			bdev, err := client.AttachNVMeController(ctx, spdk.NVMeController{Name: bdevName, TrAddr: rep.Address, TrSvcID: rep.Port, SubNQN: rep.NQN, AdrFam: adrFam})
			if err != nil {
				engineLogger.Warn().Err(err).Int("replica_index", i).Str("replica", rep.Name).Int("attempt", attempt+1).Msg("remote replica not available")
				if attempt < 4 {
					time.Sleep(10 * time.Second)
				}
				continue
			}
			engineLogger.Info().Int("replica_index", i).Str("bdev", bdev).Msg("attached remote bdev")
			time.Sleep(2 * time.Second)
			baseBdevs = append(baseBdevs, bdev)
			break
		}
	}

	if len(baseBdevs) > 0 {
		if err := client.CreateMirrorBdev(ctx, "raid0", baseBdevs, 131072); err != nil {
			return nil, err
		}
	}
	exportBdev := "raid0"

	nqn := names.VolumeNQN(config.VolumeName)
	if err := client.CreateSubsystem(ctx, nqn, "kdfs-vol", true); err != nil {
		return nil, err
	}
	for _, step := range []func() error{
		func() error { return client.AddNamespace(ctx, nqn, spdk.Namespace{BdevName: exportBdev}) },
		func() error {
			return client.AddListener(ctx, nqn, config.Listener)
		},
	} {
		if err := step(); err != nil {
			return nil, err
		}
	}

	return NewEngine(client, config), nil
}
