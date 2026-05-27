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

// Tears down existing attachments and rebuilds the SPDK subsystem to match the new replica set.
func (e *Engine) Reconfigure(ctx context.Context, newReplicas []storagev1alpha1.ReplicaAttachment) error {
	oldReplicas := e.config.Replicas

	if len(newReplicas) == len(oldReplicas) {
		same := true
		for i, r := range newReplicas {
			if r.Name != oldReplicas[i].Name {
				same = false
				break
			}
		}
		if same {
			return nil
		}
	}

	reconfigureLogger.Info().Int("old_replicas", len(oldReplicas)).Int("new_replicas", len(newReplicas)).Msg("reconfiguring engine")

	e.client.RemoveListener(ctx, e.subsystemNQN, e.listener)

	e.client.RemoveNamespace(ctx, e.subsystemNQN, 1)

	e.client.DestroyRAID(ctx, "raid0")

	for i, rep := range oldReplicas {
		if rep.IsLocal {
			continue
		}
		bdevName := fmt.Sprintf("nvme%d", i)
		if err := e.client.DetachNVMeController(ctx, bdevName); err != nil {
			reconfigureLogger.Warn().Err(err).Str("bdev", bdevName).Msg("detach NVMe failed")
		}
	}

	for i, rep := range newReplicas {
		if rep.IsLocal {
			reconfigureLogger.Info().Int("replica_index", i).Str("replica", rep.Name).Msg("replica is local on same node, skipping attach")
			continue
		}
		adrFam, err := nvmeAddressFamily(rep.Address)
		if err != nil {
			return fmt.Errorf("resolve replica address family: %w", err)
		}
		bdevName := fmt.Sprintf("nvme%d", i)
		attached := false
		for attempt := range 5 {
			_, err := e.client.AttachNVMeController(ctx, spdk.NVMeController{
				Name:    bdevName,
				TrAddr:  rep.Address,
				TrSvcID: rep.Port,
				SubNQN:  rep.NQN,
				AdrFam:  adrFam,
			})
			if err != nil {
				reconfigureLogger.Warn().Err(err).Str("bdev", bdevName).Int("attempt", attempt+1).Msg("attach NVMe failed")
				if attempt < 4 {
					time.Sleep(10 * time.Second)
				}
				continue
			}
			reconfigureLogger.Info().Str("bdev", bdevName).Msg("attached remote bdev")
			attached = true
			break
		}
		if !attached {
			reconfigureLogger.Warn().Str("bdev", bdevName).Msg("failed to attach remote bdev after retries")
		}
	}

	baseBdevs := []string{"aio0"}
	for i, rep := range newReplicas {
		if rep.IsLocal {
			continue
		}
		bdevName := fmt.Sprintf("nvme%d", i)
		baseBdevs = append(baseBdevs, bdevName)
	}

	if len(baseBdevs) > 1 {
		if err := e.client.CreateMirrorBdev(ctx, "raid0", baseBdevs, 131072); err != nil {
			return fmt.Errorf("create mirror: %w", err)
		}
	}
	exportBdev := "raid0"
	if len(baseBdevs) <= 1 {
		exportBdev = "aio0"
	}

	for _, step := range []func(){
		func() { e.client.AddNamespace(ctx, e.subsystemNQN, spdk.Namespace{BdevName: exportBdev}) },
		func() {
			e.client.AddListener(ctx, e.subsystemNQN, e.listener)
		},
	} {
		step()
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
