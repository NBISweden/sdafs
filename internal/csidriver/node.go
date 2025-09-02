package csidriver

import (
	"context"
	"fmt"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
)

func (d *Driver) NodeGetInfo(_ context.Context, r *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {

	return &csi.NodeGetInfoResponse{
		NodeId:            *d.nodeID,
		MaxVolumesPerNode: 100000,
	}, nil
}

func (d *Driver) NodeGetCapabilities(_ context.Context, r *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{},
	}, nil
}

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

	vol := &volumeInfo{ID: r.GetVolumeId(), secret: token, path: r.GetTargetPath(), context: r.GetVolumeContext()}
	d.volumes[r.GetVolumeId()] = vol

	err := d.writeToken(d, vol)
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

func (d *Driver) NodeUnpublishVolume(_ context.Context, r *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {

	vol, found := d.volumes[r.GetVolumeId()]
	if !found {
		// If we haven't seen this before, make one up
		vol = &volumeInfo{path: r.GetTargetPath()}
		d.volumes[r.GetVolumeId()] = vol
	}

	err := d.unmounter(d, vol)

	if err != nil {
		klog.V(10).Infof("NodeUnpublishVolume: unmount for %s failed: %v", vol.path, err)
		return nil, status.Error(codes.Unknown, fmt.Sprintf("Couldn't remove mountpoint %s: %v", vol.path, err))
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}
