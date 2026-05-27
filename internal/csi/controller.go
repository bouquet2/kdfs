//go:build linux

package csi

import (
	"context"
	"fmt"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	"github.com/bouquet2/kdfs/internal/names"
	"github.com/bouquet2/kdfs/internal/network"
	csipb "github.com/container-storage-interface/spec/lib/go/csi"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func isMultiNodeAccess(caps []*csipb.VolumeCapability) bool {
	for _, c := range caps {
		if c == nil || c.AccessMode == nil {
			continue
		}
		switch c.AccessMode.Mode {
		case csipb.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
			csipb.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER:
			return true
		}
	}
	return false
}

func (d *Driver) CreateVolume(ctx context.Context, req *csipb.CreateVolumeRequest) (*csipb.CreateVolumeResponse, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("volume name is required")
	}
	nodeID := d.NodeID
	if topology := req.GetAccessibilityRequirements(); topology != nil {
		for _, t := range topology.GetRequisite() {
			if node, ok := t.Segments["topology.storage.krea.to/hostname"]; ok && node != "" {
				nodeID = node
				break
			}
		}
		if nodeID == "" {
			for _, t := range topology.GetPreferred() {
				if node, ok := t.Segments["topology.storage.krea.to/hostname"]; ok && node != "" {
					nodeID = node
					break
				}
			}
		}
	}
	logger.Info().Str("name", req.Name).Str("node_id", nodeID).Bool("from_topology", req.GetAccessibilityRequirements() != nil).Str("driver_node_id", d.NodeID).Msg("create volume")
	size := fmt.Sprintf("%d", req.GetCapacityRange().GetRequiredBytes())
	volume := &storagev1alpha1.Volume{ObjectMeta: metav1.ObjectMeta{Name: req.Name, Namespace: d.Namespace}, Spec: storagev1alpha1.VolumeSpec{Size: size, StorageClassName: req.Parameters["storageClassName"], NodeID: nodeID}}
	if err := d.Client.Create(ctx, volume); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, err
	}
	vol := &csipb.Volume{
		VolumeId:      req.Name,
		CapacityBytes: req.GetCapacityRange().GetRequiredBytes(),
	}
	if !isMultiNodeAccess(req.GetVolumeCapabilities()) {
		vol.AccessibleTopology = []*csipb.Topology{
			{Segments: map[string]string{"topology.storage.krea.to/hostname": nodeID}},
		}
	}
	return &csipb.CreateVolumeResponse{Volume: vol}, nil
}

func (d *Driver) DeleteVolume(ctx context.Context, req *csipb.DeleteVolumeRequest) (*csipb.DeleteVolumeResponse, error) {
	volume := &storagev1alpha1.Volume{ObjectMeta: metav1.ObjectMeta{Name: req.VolumeId, Namespace: d.Namespace}}
	if err := d.Client.Delete(ctx, volume); err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}
	return &csipb.DeleteVolumeResponse{}, nil
}

func (d *Driver) ControllerPublishVolume(ctx context.Context, req *csipb.ControllerPublishVolumeRequest) (*csipb.ControllerPublishVolumeResponse, error) {
	engine := &storagev1alpha1.Engine{}
	if err := d.Client.Get(ctx, types.NamespacedName{Name: names.EngineName(req.VolumeId), Namespace: d.Namespace}, engine); err != nil {
		return nil, err
	}
	if engine.Status.Phase != storagev1alpha1.EnginePhaseRunning {
		return nil, fmt.Errorf("engine %s is not running", engine.Name)
	}

	pubCtx := map[string]string{"volumeId": req.VolumeId}

	accessMode := req.GetVolumeCapability().GetAccessMode().GetMode()
	switch accessMode {
	case csipb.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		csipb.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER:
		pubCtx["endpoint"] = engine.Status.Endpoint
		pubCtx["nqn"] = engine.Status.SubsystemNQN
	case csipb.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY:
		pubCtx["endpoint"] = engine.Status.Endpoint
		pubCtx["nqn"] = engine.Status.SubsystemNQN
	case csipb.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER:
		host, _, err := network.SplitEndpoint(engine.Status.Endpoint)
		if err != nil {
			return nil, err
		}
		pubCtx["protocol"] = "nfs"
		pubCtx["server"] = host
		pubCtx["export"] = "/"
	default:
		return nil, fmt.Errorf("unsupported access mode: %v", accessMode)
	}
	return &csipb.ControllerPublishVolumeResponse{PublishContext: pubCtx}, nil
}

func (d *Driver) ControllerUnpublishVolume(ctx context.Context, req *csipb.ControllerUnpublishVolumeRequest) (*csipb.ControllerUnpublishVolumeResponse, error) {
	return &csipb.ControllerUnpublishVolumeResponse{}, nil
}
