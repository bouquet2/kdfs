package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	"github.com/bouquet2/kdfs/internal/agent"
	"github.com/bouquet2/kdfs/internal/logging"
	"github.com/bouquet2/kdfs/internal/names"
	statusutil "github.com/bouquet2/kdfs/internal/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const SnapshotFinalizer = "storage.krea.to/snapshot-protection"

type SnapshotReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	StatusClient  SidecarStatusClient
	AgentFactory  func(ctx context.Context, nodeID string) (agent.Client, error)
	SnapshotURLFn func(podIP string) string
}

var snapshotLogger = logging.Component("snapshot-controller")

func (r *SnapshotReconciler) snapshotURL(podIP string) string {
	if r.SnapshotURLFn != nil {
		return r.SnapshotURLFn(podIP)
	}
	return fmt.Sprintf("http://%s:9810/snapshot", podIP)
}

func (r *SnapshotReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	snap := &storagev1alpha1.Snapshot{}
	if err := r.Get(ctx, req.NamespacedName, snap); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !snap.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, snap)
	}
	if !containsString(snap.Finalizers, SnapshotFinalizer) {
		snap.Finalizers = append(snap.Finalizers, SnapshotFinalizer)
		if err := r.Update(ctx, snap); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	switch snap.Status.Phase {
	case "":
		snap.Status.Phase = storagev1alpha1.SnapshotPhasePending
		return ctrl.Result{}, r.Status().Update(ctx, snap)
	case storagev1alpha1.SnapshotPhasePending:
		return r.startCreate(ctx, snap)
	case storagev1alpha1.SnapshotPhaseCreating:
		return r.pollCreate(ctx, snap)
	}
	return ctrl.Result{}, nil
}

func (r *SnapshotReconciler) startCreate(ctx context.Context, snap *storagev1alpha1.Snapshot) (ctrl.Result, error) {
	engine := &storagev1alpha1.Engine{}
	if err := r.Get(ctx, types.NamespacedName{Name: names.EngineName(snap.Spec.VolumeRef), Namespace: snap.Namespace}, engine); err != nil {
		snap.Status.Phase = storagev1alpha1.SnapshotPhaseFailed
		snap.Status.Conditions = statusutil.SetTrue(snap.Status.Conditions, storagev1alpha1.SnapshotConditionCreated, "EngineNotFound", "source volume engine not found")
		_ = r.Status().Update(ctx, snap)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	snap.Status.EngineNode = engine.Spec.NodeID
	snap.Status.Phase = storagev1alpha1.SnapshotPhaseCreating
	snap.Status.Conditions = statusutil.SetTrue(snap.Status.Conditions, storagev1alpha1.SnapshotConditionCreated, "Creating", "creating snapshot on engine")
	return ctrl.Result{}, r.Status().Update(ctx, snap)
}

func (r *SnapshotReconciler) pollCreate(ctx context.Context, snap *storagev1alpha1.Snapshot) (ctrl.Result, error) {
	engine := &storagev1alpha1.Engine{}
	if err := r.Get(ctx, types.NamespacedName{Name: names.EngineName(snap.Spec.VolumeRef), Namespace: snap.Namespace}, engine); err != nil {
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}
	podIP, err := r.enginePodIP(ctx, engine)
	if err != nil || podIP == "" {
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}

	payload, _ := json.Marshal(map[string]string{"snapshotID": snap.Spec.SnapshotID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.snapshotURL(podIP), bytes.NewReader(payload))
	if err != nil {
		return ctrl.Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snap.Status.Phase = storagev1alpha1.SnapshotPhaseFailed
		snap.Status.Conditions = statusutil.SetTrue(snap.Status.Conditions, storagev1alpha1.SnapshotConditionCreated, "SidecarError", fmt.Sprintf("sidecar returned %d", resp.StatusCode))
		return ctrl.Result{}, r.Status().Update(ctx, snap)
	}

	var result struct {
		SnapshotPath string `json:"snapshotPath"`
		SizeBytes    int64  `json:"sizeBytes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		snap.Status.Phase = storagev1alpha1.SnapshotPhaseFailed
		return ctrl.Result{}, r.Status().Update(ctx, snap)
	}

	now := metav1.Now()
	snap.Status.Phase = storagev1alpha1.SnapshotPhaseReady
	snap.Status.ReadyToUse = true
	snap.Status.SnapshotPath = result.SnapshotPath
	snap.Status.SizeBytes = result.SizeBytes
	snap.Status.CreationTime = &now
	snap.Status.Conditions = statusutil.SetTrue(snap.Status.Conditions, storagev1alpha1.SnapshotConditionFileReady, "Ready", "snapshot file is ready")
	return ctrl.Result{}, r.Status().Update(ctx, snap)
}

func (r *SnapshotReconciler) reconcileDelete(ctx context.Context, snap *storagev1alpha1.Snapshot) (ctrl.Result, error) {
	if snap.Status.SnapshotPath != "" && snap.Status.EngineNode != "" {
		agentClient, err := r.AgentFactory(ctx, snap.Status.EngineNode)
		if err == nil {
			_ = agentClient.DeleteSnapshot(ctx, agent.DeleteSnapshotRequest{Path: snap.Status.SnapshotPath})
		}
	}
	if containsString(snap.Finalizers, SnapshotFinalizer) {
		snap.Finalizers = removeString(snap.Finalizers, SnapshotFinalizer)
		if err := r.Update(ctx, snap); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *SnapshotReconciler) enginePodIP(ctx context.Context, engine *storagev1alpha1.Engine) (string, error) {
	if engine.Status.PodRef == nil {
		return "", nil
	}
	pod := &corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{Name: engine.Status.PodRef.Name, Namespace: engine.Status.PodRef.Namespace}, pod); err != nil {
		return "", client.IgnoreNotFound(err)
	}
	if pod.Status.Phase == corev1.PodRunning {
		return pod.Status.PodIP, nil
	}
	return "", nil
}

func (r *SnapshotReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&storagev1alpha1.Snapshot{}).Complete(r)
}

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) []string {
	var result []string
	for _, item := range slice {
		if item != s {
			result = append(result, item)
		}
	}
	return result
}
