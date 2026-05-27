package sidecar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	"github.com/bouquet2/kdfs/internal/logging"
	"github.com/bouquet2/kdfs/internal/names"
	"github.com/bouquet2/kdfs/internal/spdk"
)

var reconfigureLogger = logging.Component("sidecar-reconfigure")

type Engine struct {
	client       spdk.Client
	config       EngineConfig
	volumeName   string
	listener     spdk.Listener
	endpoint     string
	subsystemNQN string
}

func NewEngine(client spdk.Client, config EngineConfig) *Engine {
	return &Engine{
		client:       client,
		config:       config,
		volumeName:   config.VolumeName,
		listener:     config.Listener,
		endpoint:     config.Endpoint,
		subsystemNQN: names.VolumeNQN(config.VolumeName),
	}
}

func (e *Engine) Endpoint() string {
	return e.endpoint
}

func (e *Engine) Status() Status {
	return Status{Role: "engine", Endpoint: e.endpoint, SubsystemNQN: e.subsystemNQN}
}

// Reconfigure dynamically updates the RAID-1 array by adding or removing members without tearing down the subsystem.
func (e *Engine) Reconfigure(ctx context.Context, newReplicas []storagev1alpha1.ReplicaAttachment) error {
	oldReplicas := e.config.Replicas
	reconfigureLogger.Info().Int("old_replicas", len(oldReplicas)).Int("new_replicas", len(newReplicas)).Msg("reconfiguring engine")

	// 1. Get current RAID info to know what's already there
	info, err := e.client.GetRAIDInfo(ctx, "raid0")
	if err != nil {
		return fmt.Errorf("get raid info: %w", err)
	}
	baseBdevs, _ := info["base_bdevs_list"].([]any)
	currentBdevs := make(map[string]bool)
	for _, b := range baseBdevs {
		if m, ok := b.(map[string]any); ok {
			if name, ok := m["name"].(string); ok {
				currentBdevs[name] = true
			}
		}
	}

	// 2. Remove old replicas that are not in the new list
	for i, oldRep := range oldReplicas {
		if oldRep.IsLocal {
			continue
		}
		bdevName := fmt.Sprintf("nvme%d", i)
		stillNeeded := false
		for _, newRep := range newReplicas {
			if newRep.Name == oldRep.Name {
				stillNeeded = true
				break
			}
		}

		if !stillNeeded && currentBdevs[bdevName] {
			reconfigureLogger.Info().Str("bdev", bdevName).Msg("removing base bdev from RAID")
			if err := e.client.RemoveBaseBdev(ctx, "raid0", bdevName); err != nil {
				reconfigureLogger.Warn().Err(err).Str("bdev", bdevName).Msg("remove base bdev failed")
			}
			if err := e.client.DetachNVMeController(ctx, bdevName); err != nil {
				reconfigureLogger.Warn().Err(err).Str("bdev", bdevName).Msg("detach NVMe failed")
			}
		}
	}

	// 3. Add new replicas that are not in the current RAID
	for i, newRep := range newReplicas {
		if newRep.IsLocal {
			continue
		}
		bdevName := fmt.Sprintf("nvme%d", i)
		if currentBdevs[bdevName] {
			continue
		}

		reconfigureLogger.Info().Str("replica", newRep.Name).Str("bdev", bdevName).Msg("adding new replica to RAID")
		adrFam, err := nvmeAddressFamily(newRep.Address)
		if err != nil {
			return fmt.Errorf("resolve replica address family: %w", err)
		}

		attached := false
		for attempt := range 5 {
			_, err := e.client.AttachNVMeController(ctx, spdk.NVMeController{
				Name:    bdevName,
				TrAddr:  newRep.Address,
				TrSvcID: newRep.Port,
				SubNQN:  newRep.NQN,
				AdrFam:  adrFam,
			})
			if err != nil {
				reconfigureLogger.Warn().Err(err).Str("bdev", bdevName).Int("attempt", attempt+1).Msg("attach NVMe failed")
				if attempt < 4 {
					time.Sleep(5 * time.Second)
				}
				continue
			}
			attached = true
			break
		}

		if attached {
			if err := e.client.AddBaseBdev(ctx, "raid0", bdevName); err != nil {
				reconfigureLogger.Error().Err(err).Str("bdev", bdevName).Msg("failed to add base bdev to RAID")
				return fmt.Errorf("add base bdev: %w", err)
			}
			reconfigureLogger.Info().Str("bdev", bdevName).Msg("successfully added to RAID (rebuild started)")
		} else {
			return fmt.Errorf("failed to attach NVMe controller for %s", bdevName)
		}
	}

	e.config.Replicas = newReplicas
	reconfigureLogger.Info().Int("replicas", len(newReplicas)).Msg("reconfigure complete")
	return nil
}

func (e *Engine) ReconfigureHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Replicas []storagev1alpha1.ReplicaAttachment `json:"replicas"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("bad request: %v", err), http.StatusBadRequest)
		return
	}
	if err := e.Reconfigure(r.Context(), req.Replicas); err != nil {
		reconfigureLogger.Error().Err(err).Msg("reconfigure HTTP request failed")
		http.Error(w, fmt.Sprintf("reconfigure failed: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (e *Engine) StatusHTTP(w http.ResponseWriter, r *http.Request) {
	WriteStatusHTTP(w, e.Status())
}
