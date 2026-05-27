package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	"github.com/bouquet2/kdfs/internal/logging"
	"github.com/bouquet2/kdfs/internal/network"
	statusutil "github.com/bouquet2/kdfs/internal/status"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var newReconfigureHTTPClient = func() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

type EngineReconciler struct {
	client.Client
	Scheme              *runtime.Scheme
	SidecarStatusClient SidecarStatusClient
}

var engineLogger = logging.Component("engine-controller")

// Reconciles engine state by ensuring replicas are ready, creating the engine pod, and triggering reconfigure when needed.
func (r *EngineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	engine := &storagev1alpha1.Engine{}
	if err := r.Get(ctx, req.NamespacedName, engine); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	ready, err := r.replicaStatus(ctx, engine)
	if err != nil {
		return ctrl.Result{}, err
	}

	if engine.Status.PodRef == nil {
		if !ready {
			return ctrl.Result{Requeue: true}, nil
		}
		allowedHosts := r.allowedHosts(ctx, engine.Spec.NodeID)
		pod := EnginePodFor(engine, allowedHosts)
		controller := true
		blockOwnerDeletion := true
		pod.OwnerReferences = []metav1.OwnerReference{{APIVersion: storagev1alpha1.GroupVersion.String(), Kind: "Engine", Name: engine.Name, UID: engine.UID, Controller: &controller, BlockOwnerDeletion: &blockOwnerDeletion}}
		if err := r.Create(ctx, pod); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, err
		}
		engine.Status.PodRef = &storagev1alpha1.PodReference{Name: pod.Name, Namespace: pod.Namespace}
		engine.Status.Phase = storagev1alpha1.EnginePhasePending
		engine.Status.Conditions = statusutil.SetTrue(engine.Status.Conditions, storagev1alpha1.EngineConditionPodScheduled, "PodCreated", "engine pod was created")
		return ctrl.Result{Requeue: true}, r.Status().Update(ctx, engine)
	}

	if !ready {
		return ctrl.Result{Requeue: true}, nil
	}
	podIP, podExists, podReady := r.getEnginePodInfo(ctx, engine)
	if !podExists {
		engine.Status.PodRef = nil
		engine.Status.Phase = ""
		return ctrl.Result{Requeue: true}, r.Status().Update(ctx, engine)
	}
	if !podReady {
		engine.Status.Phase = storagev1alpha1.EnginePhasePending
		return ctrl.Result{Requeue: true}, nil
	}
	if podIP == "" {
		return ctrl.Result{Requeue: true}, nil
	}
	statusClient := r.SidecarStatusClient
	if statusClient == nil {
		statusClient = HTTPSidecarStatusClient{}
	}
	status, err := statusClient.GetStatus(ctx, podIP)
	if err != nil {
		return ctrl.Result{Requeue: true}, nil
	}
	if err := validateEngineSidecarStatus(status); err != nil {
		return ctrl.Result{Requeue: true}, nil
	}
	engine.Status.Endpoint = status.Endpoint
	engine.Status.SubsystemNQN = status.SubsystemNQN

	currentHash := replicasHash(engine.Spec.Replicas)
	if engine.Status.LastReplicasHash != "" && engine.Status.LastReplicasHash != currentHash {
		if err := r.triggerReconfigure(ctx, podIP, engine.Spec.Replicas); err != nil {
			engineLogger.Warn().Err(err).Str("engine", engine.Name).Msg("reconfigure failed, will retry")
			engine.Status.Phase = storagev1alpha1.EnginePhaseDegraded
			engine.Status.Conditions = statusutil.SetTrue(engine.Status.Conditions, storagev1alpha1.EngineConditionSPDKStarted, "Reconfiguring", "replica scaling in progress")
			return ctrl.Result{Requeue: true}, r.Status().Update(ctx, engine)
		}
	}
	engine.Status.LastReplicasHash = currentHash
	engine.Status.Phase = storagev1alpha1.EnginePhaseRunning
	engine.Status.Conditions = statusutil.SetTrue(engine.Status.Conditions, storagev1alpha1.EngineConditionSPDKStarted, "Started", "SPDK target is ready")
	var bdevParts []string
	for _, ra := range engine.Spec.Replicas {
		rep := &storagev1alpha1.Replica{}
		if err := r.Get(ctx, types.NamespacedName{Name: ra.Name, Namespace: engine.Namespace}, rep); err == nil {
			bdevParts = append(bdevParts, rep.Status.BdevName)
		}
	}
	engine.Status.Conditions = statusutil.SetTrue(engine.Status.Conditions, storagev1alpha1.EngineConditionRAIDConfigured, "Configured", "RAID-1 bdev is configured over "+strings.Join(bdevParts, ", "))
	engine.Status.Conditions = statusutil.SetTrue(engine.Status.Conditions, storagev1alpha1.EngineConditionSubsystemReady, "Ready", "NVMe-oF subsystem is listening")
	return ctrl.Result{}, r.Status().Update(ctx, engine)
}

func (r *EngineReconciler) replicaStatus(ctx context.Context, engine *storagev1alpha1.Engine) (bool, error) {
	for i := range engine.Spec.Replicas {
		rep := &storagev1alpha1.Replica{}
		if err := r.Get(ctx, types.NamespacedName{Name: engine.Spec.Replicas[i].Name, Namespace: engine.Namespace}, rep); err != nil {
			return false, client.IgnoreNotFound(err)
		}
		if rep.Status.Phase != storagev1alpha1.ReplicaPhaseRunning {
			return false, nil
		}
		if rep.Status.NQN == "" {
			return false, nil
		}
		pod := r.getReplicaPod(ctx, rep)
		if pod == nil {
			return false, nil
		}
		host, port, err := network.SplitEndpoint(rep.Status.Endpoint)
		if err != nil {
			return false, err
		}
		engine.Spec.Replicas[i].NQN = rep.Status.NQN
		engine.Spec.Replicas[i].Address = host
		engine.Spec.Replicas[i].IsLocal = pod.Spec.NodeName == engine.Spec.NodeID
		engine.Spec.Replicas[i].Port = port
	}
	return true, nil
}

func (r *EngineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&storagev1alpha1.Engine{}).Owns(&corev1.Pod{}).Complete(r)
}

func (r *EngineReconciler) triggerReconfigure(ctx context.Context, podIP string, replicas []storagev1alpha1.ReplicaAttachment) error {
	body := map[string]any{"replicas": replicas}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	url := reconfigureURL(podIP)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := newReconfigureHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("reconfigure returned %d", resp.StatusCode)
	}
	return nil
}
