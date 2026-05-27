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
	"github.com/bouquet2/kdfs/internal/names"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
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
			return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
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
		return ctrl.Result{}, r.Status().Update(ctx, engine)
	}

	if !ready {
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}
	podIP, podExists, podReady := r.getEnginePodInfo(ctx, engine)
	if !podExists {
		engine.Status.PodRef = nil
		engine.Status.Phase = ""
		return ctrl.Result{}, r.Status().Update(ctx, engine)
	}
	if !podReady {
		engine.Status.Phase = storagev1alpha1.EnginePhasePending
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}
	if podIP == "" {
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}
	statusClient := r.SidecarStatusClient
	if statusClient == nil {
		statusClient = HTTPSidecarStatusClient{}
	}
	status, err := statusClient.GetStatus(ctx, podIP)
	if err != nil {
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}
	if err := validateEngineSidecarStatus(status); err != nil {
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}
	engine.Status.Endpoint = status.Endpoint
	engine.Status.SubsystemNQN = status.SubsystemNQN

	currentHash := replicasHash(engine.Spec.Replicas)
	if engine.Status.LastReplicasHash != "" && engine.Status.LastReplicasHash != currentHash {
		if err := r.triggerReconfigure(ctx, podIP, engine.Spec.Replicas); err != nil {
			engineLogger.Warn().Err(err).Str("engine", engine.Name).Msg("reconfigure failed, will retry")
			engine.Status.Phase = storagev1alpha1.EnginePhaseDegraded
			engine.Status.Conditions = statusutil.SetTrue(engine.Status.Conditions, storagev1alpha1.EngineConditionSPDKStarted, "Reconfiguring", "replica scaling in progress")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, r.Status().Update(ctx, engine)
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
	allReady := true
	specChanged := false
	for i := range engine.Spec.Replicas {
		rep := &storagev1alpha1.Replica{}
		if err := r.Get(ctx, types.NamespacedName{Name: engine.Spec.Replicas[i].Name, Namespace: engine.Namespace}, rep); err != nil {
			return false, client.IgnoreNotFound(err)
		}

		pod := r.getReplicaPod(ctx, rep)
		if pod != nil && pod.Spec.NodeName != "" {
			isLocal := pod.Spec.NodeName == engine.Spec.NodeID
			if engine.Spec.Replicas[i].IsLocal != isLocal {
				engine.Spec.Replicas[i].IsLocal = isLocal
				specChanged = true
			}
		}

		if rep.Status.Phase != storagev1alpha1.ReplicaPhaseRunning || rep.Status.NQN == "" || pod == nil {
			allReady = false
			continue
		}

		host, port, err := network.SplitEndpoint(rep.Status.Endpoint)
		if err != nil {
			return false, err
		}
		if engine.Spec.Replicas[i].NQN != rep.Status.NQN || engine.Spec.Replicas[i].Address != host || engine.Spec.Replicas[i].Port != port {
			engine.Spec.Replicas[i].NQN = rep.Status.NQN
			engine.Spec.Replicas[i].Address = host
			engine.Spec.Replicas[i].Port = port
			specChanged = true
		}
	}
	if specChanged {
		if err := r.Update(ctx, engine); err != nil {
			return false, err
		}
	}
	return allReady, nil
}

func (r *EngineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&storagev1alpha1.Engine{}).
		Owns(&corev1.Pod{}).
		Watches(&corev1.Event{}, handler.EnqueueRequestsFromMapFunc(r.mapEventToEngine)).
		Complete(r)
}

func (r *EngineReconciler) mapEventToEngine(ctx context.Context, obj client.Object) []ctrl.Request {
	event, ok := obj.(*corev1.Event)
	if !ok {
		return nil
	}
	if event.Reason != "Scheduled" || event.InvolvedObject.Kind != "Pod" {
		return nil
	}
	podName := event.InvolvedObject.Name
	if !strings.HasSuffix(podName, "-pod") {
		return nil
	}
	// Replica pod names are volume-replica-index-pod
	parts := strings.Split(podName, "-replica-")
	if len(parts) < 2 {
		return nil
	}
	volumeName := parts[0]
	return []ctrl.Request{
		{NamespacedName: types.NamespacedName{
			Name:      names.EngineName(volumeName),
			Namespace: event.InvolvedObject.Namespace,
		}},
	}
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
