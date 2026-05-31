package controller

import (
	"context"
	"fmt"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	"github.com/bouquet2/kdfs/internal/names"
	statusutil "github.com/bouquet2/kdfs/internal/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Adds new replica CRs on unused nodes and updates engine spec to match the desired replica count.
func (r *VolumeReconciler) scaleUp(ctx context.Context, volume *storagev1alpha1.Volume, engine *storagev1alpha1.Engine, current, desired int) error {
	usedNodes := map[string]bool{}
	usedIndices := map[int]bool{}
	for _, ra := range engine.Spec.Replicas {
		usedNodes[ra.NodeID] = true
		for idx := 0; idx < desired+10; idx++ {
			if ra.Name == names.ReplicaName(volume.Name, idx) {
				usedIndices[idx] = true
				break
			}
		}
	}
	owner := metav1.OwnerReference{APIVersion: storagev1alpha1.GroupVersion.String(), Kind: "Volume", Name: volume.Name, UID: volume.UID}
	nextIndex := 0
	for i := current; i < desired; i++ {
		for usedIndices[nextIndex] {
			nextIndex++
		}
		node := r.pickUnusedNode(ctx, usedNodes)
		if volume.Spec.SnapshotSource != nil {
			node = volume.Spec.NodeID
		}
		usedNodes[node] = true
		replicaName := names.ReplicaName(volume.Name, nextIndex)
		usedIndices[nextIndex] = true
		replicaType := storagev1alpha1.ReplicaTypeRemote
		if node == volume.Spec.NodeID {
			replicaType = storagev1alpha1.ReplicaTypeLocal
		}
		spec := storagev1alpha1.ReplicaSpec{
			VolumeRef: storagev1alpha1.LocalObjectReference{Name: volume.Name},
			NodeID:    node,
			Type:      replicaType,
			Size:      volume.Spec.Size,
			DataPath:  names.DataPath(volume.Name),
		}
		if volume.Spec.SnapshotSource != nil {
			spec.SnapshotSource = volume.Spec.SnapshotSource.SnapshotName
		}
		replica := &storagev1alpha1.Replica{
			ObjectMeta: metav1.ObjectMeta{Name: replicaName, Namespace: volume.Namespace, OwnerReferences: []metav1.OwnerReference{owner}},
			Spec:       spec,
		}
		if err := r.createIfMissing(ctx, replica); err != nil {
			return err
		}
		engine.Spec.Replicas = append(engine.Spec.Replicas, storagev1alpha1.ReplicaAttachment{
			Name:   replicaName,
			NodeID: node,
		})
	}
	if err := r.Update(ctx, engine); err != nil {
		return err
	}
	volume.Status.Phase = storagev1alpha1.VolumePhaseDegraded
	volume.Status.Conditions = statusutil.SetTrue(volume.Status.Conditions, storagev1alpha1.VolumeConditionScheduled, "ScalingUp", fmt.Sprintf("scaling from %d to %d replicas", current, desired))
	return r.Status().Update(ctx, volume)
}

func (r *VolumeReconciler) scaleDown(ctx context.Context, volume *storagev1alpha1.Volume, engine *storagev1alpha1.Engine, current, desired int) error {
	for i := desired; i < current; i++ {
		replicaName := names.ReplicaName(volume.Name, i)
		replica := &storagev1alpha1.Replica{}
		replica.Name = replicaName
		replica.Namespace = volume.Namespace
		if err := r.Delete(ctx, replica); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	engine.Spec.Replicas = engine.Spec.Replicas[:desired]
	if err := r.Update(ctx, engine); err != nil {
		return err
	}
	volume.Status.Phase = storagev1alpha1.VolumePhaseDegraded
	volume.Status.Conditions = statusutil.SetTrue(volume.Status.Conditions, storagev1alpha1.VolumeConditionScheduled, "ScalingDown", fmt.Sprintf("scaling from %d to %d replicas", current, desired))
	return r.Status().Update(ctx, volume)
}
