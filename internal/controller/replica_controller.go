package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	"github.com/bouquet2/kdfs/internal/agent"
	statusutil "github.com/bouquet2/kdfs/internal/status"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ReplicaReconciler struct {
	client.Client
	Scheme              *runtime.Scheme
	Agent               agent.Client
	AgentFactory        func(context.Context, string) (agent.Client, error)
	SidecarStatusClient SidecarStatusClient
}

// Ensures a replica is provisioned on the target node and updates status once the backing resources exist.
func (r *ReplicaReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	replica := &storagev1alpha1.Replica{}
	if err := r.Get(ctx, req.NamespacedName, replica); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if replica.Status.Phase == storagev1alpha1.ReplicaPhaseRunning && replica.Spec.Type == storagev1alpha1.ReplicaTypeRemote {
		pod, err := r.replicaPod(ctx, replica)
		if err == nil && pod != nil && pod.Status.PodIP != "" {
			return ctrl.Result{}, nil
		}
	}
	agentClient := r.Agent
	if agentClient == nil && r.AgentFactory != nil {
		var err error
		agentClient, err = r.AgentFactory(ctx, replica.Spec.NodeID)
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	if agentClient == nil {
		return ctrl.Result{}, fmt.Errorf("replica reconciler agent client is not configured")
	}
	snapPath := ""
	if replica.Spec.SnapshotSource != "" {
		snap := &storagev1alpha1.Snapshot{}
		if err := r.Get(ctx, types.NamespacedName{Name: replica.Spec.SnapshotSource, Namespace: replica.Namespace}, snap); err == nil {
			snapPath = snap.Status.SnapshotPath
		}
	}
	if _, err := agentClient.CreateReplica(ctx, agent.CreateReplicaRequest{
		Path:           replica.Spec.DataPath,
		Size:           replica.Spec.Size,
		SnapshotSource: snapPath,
	}); err != nil {
		return ctrl.Result{}, err
	}
	if replica.Spec.Type == storagev1alpha1.ReplicaTypeRemote {
		pod := ReplicaPodFor(replica)
		pod.OwnerReferences = []metav1.OwnerReference{{APIVersion: storagev1alpha1.GroupVersion.String(), Kind: "Replica", Name: replica.Name, UID: replica.UID}}
		if err := r.Create(ctx, pod); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, err
		}
		pod, err := r.replicaPod(ctx, replica)
		if err != nil {
			return ctrl.Result{}, err
		}
		if pod == nil || pod.Status.PodIP == "" {
			return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
		}
		statusClient := r.SidecarStatusClient
		if statusClient == nil {
			statusClient = HTTPSidecarStatusClient{}
		}
		status, err := statusClient.GetStatus(ctx, pod.Status.PodIP)
		if err != nil {
			return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
		}
		if err := validateReplicaSidecarStatus(status); err != nil {
			return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
		}
		replica.Status.NQN = status.ReplicaNQN
		replica.Status.Endpoint = status.Endpoint
		replica.Status.Conditions = statusutil.SetTrue(replica.Status.Conditions, storagev1alpha1.ReplicaConditionNVMFExported, "Exported", "remote replica is exported over NVMe-oF")
	} else {
		// Clean up pod if it exists (e.g. transitioned from Remote to Local)
		pod, err := r.replicaPod(ctx, replica)
		if err == nil && pod != nil {
			if err := r.Delete(ctx, pod); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
		replica.Status.NQN = ""
		replica.Status.Endpoint = ""
	}
	replica.Status.Phase = storagev1alpha1.ReplicaPhaseRunning
	replica.Status.BdevName = "aio0"
	replica.Status.Conditions = statusutil.SetTrue(replica.Status.Conditions, storagev1alpha1.ReplicaConditionFilesystemCreated, "Created", "replica backing file was created")
	replica.Status.Conditions = statusutil.SetTrue(replica.Status.Conditions, storagev1alpha1.ReplicaConditionBdevAttached, "Attached", "replica backing bdev is attached")
	return ctrl.Result{}, r.Status().Update(ctx, replica)
}

func (r *ReplicaReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&storagev1alpha1.Replica{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}

func replicaIndexFromName(name string) int {
	var i int
	parts := strings.Split(name, "-")
	for idx, part := range parts {
		if part == "replica" && idx+1 < len(parts) {
			fmt.Sscanf(parts[idx+1], "%d", &i)
			return i
		}
	}
	return 0
}
