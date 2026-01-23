package csidriver

import (
	"context"
	"fmt"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/tj/assert"
)

func TestNodeGetInfo(t *testing.T) {
	nodeid := "nodeid"
	d := Driver{nodeID: &nodeid}

	r, err := d.NodeGetInfo(context.TODO(), &csi.NodeGetInfoRequest{})
	assert.Equal(t, nil, err, "Unexpected error from NodeGetInfo")
	assert.Equal(t, "nodeid", r.NodeId, "Unexpected node id from NodeGetInfo")
}

func TestNodeGetCapabilities(t *testing.T) {
	d := Driver{}

	r, err := d.NodeGetCapabilities(context.TODO(), &csi.NodeGetCapabilitiesRequest{})
	assert.Equal(t, nil, err, "Unexpected error from NodeGetCapabilities")
	assert.Equal(t, 1, len(r.Capabilities), "Unexpected length of capabilities"+
		"from NodeGetCapabilities")
}

func TestNodePublishVolume(t *testing.T) {
	tokenDirectory := "/tmp"
	d := Driver{
		tokenDir: &tokenDirectory,
		volumes:  make(map[string]*volumeInfo),
	}
	secrets := make(map[string]string)

	d.mounter = goodMount
	d.writeToken = badMount

	req := csi.NodePublishVolumeRequest{
		Secrets:       secrets,
		VolumeId:      "id",
		TargetPath:    "/path",
		VolumeContext: make(map[string]string),
	}

	// No token yet
	_, err := d.NodePublishVolume(context.TODO(), &req)
	assert.NotNil(t, err, "Unexpected lack of failure from NodePublishVolume "+
		"when missing token")

	// Now writeToken failure should trigger an error
	secrets["token"] = "testtoken"
	_, err = d.NodePublishVolume(context.TODO(), &req)
	assert.NotNil(t, err, "Unexpected lack of error from NodePublishVolume "+
		"with bad token write")

	d.writeToken = goodMount
	// This should work now
	secrets["token"] = "testtoken"
	r, err := d.NodePublishVolume(context.TODO(), &req)
	assert.Equal(t, nil, err, "Unexpected error from NodePublishVolume "+
		"when things should work")
	assert.NotNil(t, r, "Unexpected bad return value from NodePublishVolume "+
		"when things should work")

	// Mount fails?
	d.mounter = badMount
	_, err = d.NodePublishVolume(context.TODO(), &req)
	assert.NotNil(t, err, "Unexpected lack of error from NodePublishVolume "+
		"with bad mount")
}

func goodMount(d *Driver, v *volumeInfo) error {
	return nil
}

func badMount(d *Driver, v *volumeInfo) error {
	return fmt.Errorf("fail")
}

func TestNodeUnpublishVolume(t *testing.T) {

	d := Driver{
		volumes: make(map[string]*volumeInfo),
	}

	d.unmounter = goodMount

	req := csi.NodeUnpublishVolumeRequest{

		VolumeId:   "id",
		TargetPath: "/path",
	}

	// No token yet
	r, err := d.NodeUnpublishVolume(context.TODO(), &req)
	assert.Equal(t, nil, err, "Unexpected error from NodeUnpublishVolume "+
		"for good mount")
	assert.NotNil(t, r, "Missing return for good mount for NodeUnpublishVolume")
	// This should work now

	r, err = d.NodeUnpublishVolume(context.TODO(), &req)
	assert.Equal(t, nil, err, "Unexpected error from NodeUnpublishVolume "+
		"for good mount with volumes")
	assert.NotNil(t, r, "Missing return for good mount for NodeUnpublishVolume")

	d.unmounter = badMount
	r, err = d.NodeUnpublishVolume(context.TODO(), &req)
	assert.NotNil(t, err, "Unexpected lack of error from NodeUnpublishVolume "+
		"with bad mount")
	assert.Nil(t, r, "Unexpected return for bad mount for NodeUnpublishVolume")
}
