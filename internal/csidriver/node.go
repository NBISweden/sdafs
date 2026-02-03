package csidriver

import (
	"context"
	"fmt"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
)

// maxVolumesPerNodeDefault is what we report as MaxVolumesPerNode in
// NodeGetInfo
const maxVolumesPerNodeDefault = 100000

// NodeGetInfo reports node information
func (d *Driver) NodeGetInfo(_ context.Context, r *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	return &csi.NodeGetInfoResponse{
		NodeId:            *d.nodeID,
		MaxVolumesPerNode: maxVolumesPerNodeDefault,
	}, nil
}

func getNodeCapabilites() (c []*csi.NodeServiceCapability) {

	for _, capability := range []csi.NodeServiceCapability_RPC_Type{
		csi.NodeServiceCapability_RPC_VOLUME_MOUNT_GROUP,
	} {

		c = append(c, &csi.NodeServiceCapability{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: capability,
				},
			},
		})
	}

	return c
}

// NodeGetCapabilities reports node capabilities
func (d *Driver) NodeGetCapabilities(_ context.Context, r *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: getNodeCapabilites(),
	}, nil
}

// NodePublishVolume publishes (mounts) the given volume for use by a pod
func (d *Driver) NodePublishVolume(_ context.Context, r *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	secrets := r.GetSecrets()

	tokenKey := "token"

	fromContext, found := r.GetVolumeContext()["tokenkey"]
	if found {
		tokenKey = fromContext
	}

	token, ok := secrets[tokenKey]

	if !ok {
		klog.V(10).Infof("NodePublishVolume: secret misses key '%s' missing so can't authenticate, giving up", tokenKey)
		return nil, status.Error(codes.Unauthenticated, "Expected key not found in received Secret")
	}

	var group string
	if r.GetVolumeCapability() != nil && r.GetVolumeCapability().GetMount() != nil {
		group = r.GetVolumeCapability().GetMount().GetVolumeMountGroup()
	}

	vol := &volumeInfo{ID: r.GetVolumeId(),
		Secret:  token,
		Path:    r.GetTargetPath(),
		Context: r.GetVolumeContext(),
		Group:   group,
	}
	d.volumes[r.GetVolumeId()] = vol

	err := d.writePersisted(vol)
	if err != nil {
		klog.V(10).Infof("NodePublishVolume: couldn't write pesistence data for mounter, giving up: %v", err)
		return nil, status.Error(codes.Unknown, fmt.Sprintf("Persistence creation failed: %v", err))
	}

	err = d.writeToken(d, vol)
	if err != nil {
		klog.V(10).Infof("NodePublishVolume: couldn't write token secret for mounter, giving up: %v", err)
		return nil, status.Error(codes.Unknown, fmt.Sprintf("Token creation failed: %v", err))
	}

	err = d.mounter(d, vol)

	if err != nil {
		klog.V(10).Infof("NodePublishVolume: couldn't mount sdafs, giving up: %v", err)
		return nil, status.Error(codes.Unknown, fmt.Sprintf("sdafs mount failed: %v", err))
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

// NodeUnpublishVolume unpublishes (unmounts) the given volume
func (d *Driver) NodeUnpublishVolume(_ context.Context, r *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {

	vol, found := d.volumes[r.GetVolumeId()]
	if !found {
		// If we haven't seen this before, make one up
		// we can't fall out since we might not know about everything due
		// to restart and us not trying to keep any persistent state
		vol = &volumeInfo{Path: r.GetTargetPath()}
		d.volumes[r.GetVolumeId()] = vol
	}

	err := d.unmounter(d, vol)

	if err != nil {
		klog.V(10).Infof("NodeUnpublishVolume: unmount for %s failed: %v", vol.Path, err)
		return nil, status.Error(codes.Unknown, fmt.Sprintf("Couldn't remove mountpoint %s: %v", vol.Path, err))
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}
