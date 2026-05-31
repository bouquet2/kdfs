//go:build linux

package csi

import (
	"context"

	csipb "github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const DriverName = "storage.krea.to"

var DriverVersion = "0.1.0"

type Mounter interface {
	Mount(source, target, fsType string, options []string) error
	BindMount(source, target string, readonly bool) error
	Unmount(target string) error
}

type ExecMounter struct{}

type Driver struct {
	csipb.UnimplementedIdentityServer
	csipb.UnimplementedControllerServer
	csipb.UnimplementedNodeServer
	Client    client.Client
	Namespace string
	NodeID    string
	Mounter   Mounter
}

func (d *Driver) GetPluginInfo(ctx context.Context, req *csipb.GetPluginInfoRequest) (*csipb.GetPluginInfoResponse, error) {
	return &csipb.GetPluginInfoResponse{
		Name:          DriverName,
		VendorVersion: DriverVersion,
	}, nil
}

func (d *Driver) GetPluginCapabilities(ctx context.Context, req *csipb.GetPluginCapabilitiesRequest) (*csipb.GetPluginCapabilitiesResponse, error) {
	return &csipb.GetPluginCapabilitiesResponse{
		Capabilities: []*csipb.PluginCapability{
			{
				Type: &csipb.PluginCapability_Service_{
					Service: &csipb.PluginCapability_Service{
						Type: csipb.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
			{
				Type: &csipb.PluginCapability_Service_{
					Service: &csipb.PluginCapability_Service{
						Type: csipb.PluginCapability_Service_VOLUME_ACCESSIBILITY_CONSTRAINTS,
					},
				},
			},
		},
	}, nil
}

func (d *Driver) Probe(ctx context.Context, req *csipb.ProbeRequest) (*csipb.ProbeResponse, error) {
	return &csipb.ProbeResponse{Ready: &wrapperspb.BoolValue{Value: true}}, nil
}

func (d *Driver) ControllerGetCapabilities(ctx context.Context, req *csipb.ControllerGetCapabilitiesRequest) (*csipb.ControllerGetCapabilitiesResponse, error) {
	return &csipb.ControllerGetCapabilitiesResponse{
		Capabilities: []*csipb.ControllerServiceCapability{
			{Type: &csipb.ControllerServiceCapability_Rpc{Rpc: &csipb.ControllerServiceCapability_RPC{Type: csipb.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME}}},
			{Type: &csipb.ControllerServiceCapability_Rpc{Rpc: &csipb.ControllerServiceCapability_RPC{Type: csipb.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME}}},
			{Type: &csipb.ControllerServiceCapability_Rpc{Rpc: &csipb.ControllerServiceCapability_RPC{Type: csipb.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT}}},
			{Type: &csipb.ControllerServiceCapability_Rpc{Rpc: &csipb.ControllerServiceCapability_RPC{Type: csipb.ControllerServiceCapability_RPC_LIST_SNAPSHOTS}}},
		},
	}, nil
}

func (d *Driver) NodeGetCapabilities(ctx context.Context, req *csipb.NodeGetCapabilitiesRequest) (*csipb.NodeGetCapabilitiesResponse, error) {
	return &csipb.NodeGetCapabilitiesResponse{
		Capabilities: []*csipb.NodeServiceCapability{
			{Type: &csipb.NodeServiceCapability_Rpc{Rpc: &csipb.NodeServiceCapability_RPC{Type: csipb.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME}}},
		},
	}, nil
}

func (d *Driver) NodeGetInfo(ctx context.Context, req *csipb.NodeGetInfoRequest) (*csipb.NodeGetInfoResponse, error) {
	return &csipb.NodeGetInfoResponse{
		NodeId:            d.NodeID,
		MaxVolumesPerNode: 64,
		AccessibleTopology: &csipb.Topology{
			Segments: map[string]string{
				"topology.storage.krea.to/hostname": d.NodeID,
			},
		},
	}, nil
}
