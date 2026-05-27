package controller

import (
	"context"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	"github.com/bouquet2/kdfs/internal/names"
	statusutil "github.com/bouquet2/kdfs/internal/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type VolumeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Drives the volume lifecycle: create children, wait for readiness, ensure scale, and trigger healing when degraded.
func (r *VolumeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	volume := &storagev1alpha1.Volume{}
	if err := r.Get(ctx, req.NamespacedName, volume); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if volume.Status.EngineRef == nil {
		localNode := volume.Spec.NodeID
		if localNode == "" {
			node, err := r.pickAnyWorkerNode(ctx)
			if err != nil {
				return ctrl.Result{}, err
			}
			localNode = node
			volume.Spec.NodeID = localNode
			if err := r.Update(ctx, volume); err != nil {
				return ctrl.Result{}, err
			}
		}
		n, err := r.replicasForVolume(ctx, volume)
		if err != nil {
			return ctrl.Result{}, err
		}
		nodes, err := r.pickWorkerNodes(ctx, n)
		if err != nil {
			return ctrl.Result{}, err
		}
		if err := r.ensureChildren(ctx, volume, nodes); err != nil {
			return ctrl.Result{}, err
		}
		volume.Status.EngineRef = &storagev1alpha1.NamespacedObjectReference{Name: names.EngineName(volume.Name), Namespace: volume.Namespace}
		volume.Status.Phase = storagev1alpha1.VolumePhaseCreating
		volume.Status.Conditions = statusutil.SetTrue(volume.Status.Conditions, storagev1alpha1.VolumeConditionScheduled, "ChildrenCreated", "engine and replicas were created")
		return ctrl.Result{}, r.Status().Update(ctx, volume)
	}

	ready, err := r.childrenReady(ctx, volume)
	if err != nil {
		return ctrl.Result{}, err
	}
	if ready && volume.Status.Phase != storagev1alpha1.VolumePhaseReady {
		volume.Status.Phase = storagev1alpha1.VolumePhaseReady
		volume.Status.Conditions = statusutil.SetTrue(volume.Status.Conditions, storagev1alpha1.VolumeConditionEngineReady, "EngineRunning", "engine is running")
		volume.Status.Conditions = statusutil.SetTrue(volume.Status.Conditions, storagev1alpha1.VolumeConditionReplicasReady, "ReplicasRunning", "all replicas are running")
		return ctrl.Result{}, r.Status().Update(ctx, volume)
	}
	engine := &storagev1alpha1.Engine{}
	if err := r.Get(ctx, types.NamespacedName{Name: names.EngineName(volume.Name), Namespace: volume.Namespace}, engine); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}
	desired, err := r.replicasForVolume(ctx, volume)
	if err != nil {
		return ctrl.Result{}, err
	}
	if desired != len(engine.Spec.Replicas) {
		if err := r.ensureScale(ctx, volume, engine); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	if !ready {
		healed, requeueAfter, healErr := r.healReplicas(ctx, volume, engine)
		if healErr != nil {
			return ctrl.Result{}, healErr
		}
		if healed {
			return ctrl.Result{}, nil
		}
		if requeueAfter > 0 {
			return ctrl.Result{RequeueAfter: requeueAfter}, nil
		}
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

func (r *VolumeReconciler) ensureChildren(ctx context.Context, volume *storagev1alpha1.Volume, nodes []string) error {
	owner := metav1.OwnerReference{APIVersion: storagev1alpha1.GroupVersion.String(), Kind: "Volume", Name: volume.Name, UID: volume.UID}
	replicas := make([]storagev1alpha1.ReplicaAttachment, 0, len(nodes))

	for i, node := range nodes {
		replicaName := names.ReplicaName(volume.Name, i)
		replica := &storagev1alpha1.Replica{
			ObjectMeta: metav1.ObjectMeta{Name: replicaName, Namespace: volume.Namespace, OwnerReferences: []metav1.OwnerReference{owner}},
			Spec: storagev1alpha1.ReplicaSpec{
				VolumeRef: storagev1alpha1.LocalObjectReference{Name: volume.Name},
				NodeID:    node,
				Type:      storagev1alpha1.ReplicaTypeRemote,
				Size:      volume.Spec.Size,
				DataPath:  names.DataPath(volume.Name),
			},
		}
		if err := r.createIfMissing(ctx, replica); err != nil {
			return err
		}
		replicas = append(replicas, storagev1alpha1.ReplicaAttachment{
			Name:   replicaName,
			NodeID: node,
		})
	}

	engine := &storagev1alpha1.Engine{
		ObjectMeta: metav1.ObjectMeta{Name: names.EngineName(volume.Name), Namespace: volume.Namespace, OwnerReferences: []metav1.OwnerReference{owner}},
		Spec: storagev1alpha1.EngineSpec{
			VolumeRef: storagev1alpha1.LocalObjectReference{Name: volume.Name},
			NodeID:    volume.Spec.NodeID,
			Replicas:  replicas,
		},
	}
	return r.createIfMissing(ctx, engine)
}

func (r *VolumeReconciler) ensureScale(ctx context.Context, volume *storagev1alpha1.Volume, engine *storagev1alpha1.Engine) error {
	desired, err := r.replicasForVolume(ctx, volume)
	if err != nil {
		return err
	}
	current := len(engine.Spec.Replicas)
	if desired == current {
		return nil
	}

	if desired > current {
		return r.scaleUp(ctx, volume, engine, current, desired)
	}
	return r.scaleDown(ctx, volume, engine, current, desired)
}

func (r *VolumeReconciler) createIfMissing(ctx context.Context, obj client.Object) error {
	if err := r.Create(ctx, obj); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (r *VolumeReconciler) childrenReady(ctx context.Context, volume *storagev1alpha1.Volume) (bool, error) {
	engine := &storagev1alpha1.Engine{}
	if err := r.Get(ctx, types.NamespacedName{Name: names.EngineName(volume.Name), Namespace: volume.Namespace}, engine); err != nil {
		return false, client.IgnoreNotFound(err)
	}
	if engine.Status.Phase != storagev1alpha1.EnginePhaseRunning {
		return false, nil
	}
	for _, attach := range engine.Spec.Replicas {
		replica := &storagev1alpha1.Replica{}
		if err := r.Get(ctx, types.NamespacedName{Name: attach.Name, Namespace: volume.Namespace}, replica); err != nil {
			return false, client.IgnoreNotFound(err)
		}
		if replica.Status.Phase != storagev1alpha1.ReplicaPhaseRunning {
			return false, nil
		}
	}
	return true, nil
}

func (r *VolumeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&storagev1alpha1.Volume{}).Owns(&storagev1alpha1.Engine{}).Owns(&storagev1alpha1.Replica{}).Complete(r)
}
