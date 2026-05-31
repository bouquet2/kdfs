//go:build linux

package csi

import (
	"context"
	"fmt"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	csipb "github.com/container-storage-interface/spec/lib/go/csi"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"google.golang.org/protobuf/types/known/timestamppb"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (d *Driver) CreateSnapshot(ctx context.Context, req *csipb.CreateSnapshotRequest) (*csipb.CreateSnapshotResponse, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("snapshot name is required")
	}
	if req.SourceVolumeId == "" {
		return nil, fmt.Errorf("source volume ID is required")
	}

	snap := &storagev1alpha1.Snapshot{
		ObjectMeta: metav1.ObjectMeta{Name: req.Name, Namespace: d.Namespace},
		Spec:       storagev1alpha1.SnapshotSpec{VolumeRef: req.SourceVolumeId, SnapshotID: req.Name},
	}
	if err := d.Client.Create(ctx, snap); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return nil, err
		}
		if err := d.Client.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: d.Namespace}, snap); err != nil {
			return nil, err
		}
		if snap.Spec.VolumeRef != req.SourceVolumeId {
			return nil, fmt.Errorf("snapshot %s exists for different volume %s", req.Name, snap.Spec.VolumeRef)
		}
	}

	result := &csipb.Snapshot{
		SnapshotId:     snap.Spec.SnapshotID,
		SourceVolumeId: snap.Spec.VolumeRef,
		ReadyToUse:     snap.Status.ReadyToUse,
		SizeBytes:      snap.Status.SizeBytes,
	}
	if snap.Status.CreationTime != nil {
		result.CreationTime = timestamppb.New(snap.Status.CreationTime.Time)
	}
	return &csipb.CreateSnapshotResponse{Snapshot: result}, nil
}

func (d *Driver) DeleteSnapshot(ctx context.Context, req *csipb.DeleteSnapshotRequest) (*csipb.DeleteSnapshotResponse, error) {
	snap := &storagev1alpha1.Snapshot{ObjectMeta: metav1.ObjectMeta{Name: req.SnapshotId, Namespace: d.Namespace}}
	if err := d.Client.Delete(ctx, snap); err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}
	return &csipb.DeleteSnapshotResponse{}, nil
}

func (d *Driver) ListSnapshots(ctx context.Context, req *csipb.ListSnapshotsRequest) (*csipb.ListSnapshotsResponse, error) {
	var list storagev1alpha1.SnapshotList

	if snapID := req.GetSnapshotId(); snapID != "" {
		snap := &storagev1alpha1.Snapshot{}
		if err := d.Client.Get(ctx, types.NamespacedName{Name: snapID, Namespace: d.Namespace}, snap); err != nil {
			return &csipb.ListSnapshotsResponse{}, client.IgnoreNotFound(err)
		}
		list.Items = []storagev1alpha1.Snapshot{*snap}
	} else {
		if err := d.Client.List(ctx, &list, client.InNamespace(d.Namespace)); err != nil {
			return nil, err
		}
		if volID := req.GetSourceVolumeId(); volID != "" {
			var filtered []storagev1alpha1.Snapshot
			for _, s := range list.Items {
				if s.Spec.VolumeRef == volID {
					filtered = append(filtered, s)
				}
			}
			list.Items = filtered
		}
	}

	entries := make([]*csipb.ListSnapshotsResponse_Entry, 0, len(list.Items))
	for _, snap := range list.Items {
		csiSnap := &csipb.Snapshot{
			SnapshotId:     snap.Spec.SnapshotID,
			SourceVolumeId: snap.Spec.VolumeRef,
			ReadyToUse:     snap.Status.ReadyToUse,
			SizeBytes:      snap.Status.SizeBytes,
		}
		if snap.Status.CreationTime != nil {
			csiSnap.CreationTime = timestamppb.New(snap.Status.CreationTime.Time)
		}
		entries = append(entries, &csipb.ListSnapshotsResponse_Entry{Snapshot: csiSnap})
	}
	return &csipb.ListSnapshotsResponse{Entries: entries}, nil
}
