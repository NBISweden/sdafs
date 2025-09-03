package csidriver

import (
	"context"
	"io/fs"
	"os"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/google/uuid"
	"github.com/tj/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pluginregistration "k8s.io/kubelet/pkg/apis/pluginregistration/v1"
)

func TestRegisterKubeletAndPluginRegistration(t *testing.T) {
	illegalEndpoint := "unix:/dev"
	randomString := uuid.New().String()

	d := Driver{
		endpoint:             &randomString,
		registrationEndpoint: &illegalEndpoint,
	}

	err := d.registerKubelet()
	assert.NotNil(t, err, "Unusable socket path should fail")

	socket := "socket-" + uuid.New().String()
	d.registrationEndpoint = &socket
	defer os.Remove(*d.registrationEndpoint) // nolint:errcheck

	err = d.registerKubelet()
	assert.Equal(t, err, nil, "Unexpected error from registerKubelet")

	// We should have the server running now
	conn, err := grpc.NewClient("unix:"+socket, grpc.WithTransportCredentials(insecure.NewCredentials()))
	assert.Nil(t, err, "Error while setting up grpc client")

	defer conn.Close() // nolint:errcheck
	regclient := pluginregistration.NewRegistrationClient(conn)
	assert.NotNil(t, regclient, "Making a grpc client failed")

	infoResponse, err := regclient.GetInfo(context.TODO(), &pluginregistration.InfoRequest{})
	t.Logf("err: %v", err)
	assert.Nil(t, err, "Error while calling GetInfo")
	assert.NotNil(t, infoResponse, "Bad response from GetInfo")
	assert.Equal(t, infoResponse.Type, pluginregistration.CSIPlugin, "Unexpected type from GetInfo")
	assert.Equal(t, infoResponse.SupportedVersions, VERSIONS, "Unexpected version from GetInfo")
	assert.Equal(t, infoResponse.Name, DRIVERNAME, "Unexpected name from GetInfo")
	assert.Equal(t, infoResponse.Endpoint, randomString, "Unexpected endpoint from GetInfo")

	notifyResponse, err := regclient.NotifyRegistrationStatus(context.TODO(), &pluginregistration.RegistrationStatus{})
	assert.Nil(t, err, "Error while calling NotifyRegistrationStatus")
	assert.NotNil(t, notifyResponse, "Bad response from NotifyRegistrationStatus")
}

func TestNewDriver(t *testing.T) {
	illegalEndpoint := "/dev"
	nodeid := uuid.New().String()
	endpoint := "socket-" + uuid.New().String()
	registerendpoint := "rsocket-" + uuid.New().String()
	tokendir := uuid.New().String()
	logdir := uuid.New().String()
	sdafspath := uuid.New().String()
	boolVal := true

	c := CSIConfig{
		NodeID:    &nodeid,
		TokenDir:  &tokendir,
		LogDir:    &logdir,
		SdafsPath: &sdafspath,
		WorldOpen: &boolVal,
	}
	_, err := NewDriver(&c)
	assert.NotNil(t, err, "NewDriver should fail when no sockets given")

	c.Endpoint = &illegalEndpoint
	_, err = NewDriver(&c)
	assert.NotNil(t, err, "NewDriver should fail for bad socket")

	c.Endpoint = &endpoint
	c.RegistrationEndpoint = &registerendpoint

	d, err := NewDriver(&c)

	assert.Nil(t, err, "Error while calling NewDriver")
	assert.Equal(t, d.nodeID, c.NodeID, "NodeID mismatch")
	assert.Equal(t, d.tokenDir, c.TokenDir, "TokenDir mismatch")
	assert.Equal(t, d.logDir, c.LogDir, err, "LogDir mismatch")
	assert.Equal(t, d.sdafsPath, c.SdafsPath, err, "SdafsPath mismatch")
	assert.Equal(t, d.registrationEndpoint, c.RegistrationEndpoint, err,
		"RegistrationEndpoint mismatch")
	assert.Equal(t, d.endpoint, c.Endpoint, err, "Endpoint mismatch")
	assert.NotNil(t, d.volumes, "Volumes map isn't initialised")
}

func TestRun(t *testing.T) {
	failEndpoint := "unix:/dev"
	endpoint := "runsocket-" + uuid.New().String()
	regEndpoint := "runregsocket-" + uuid.New().String()
	nodeid := "node-" + uuid.New().String()

	d := Driver{
		endpoint:             &failEndpoint,
		registrationEndpoint: &failEndpoint,
		nodeID:               &nodeid,
	}

	// Cleanup afterwards
	defer os.Remove(endpoint)    // nolint:errcheck
	defer os.Remove(regEndpoint) // nolint:errcheck

	err := d.Run()
	assert.NotNil(t, err, "Run should fail for bad endpoint")

	d.endpoint = &endpoint
	err = d.Run()
	assert.NotNil(t, err, "Run should fail for bad registration endpoint")

	d.registrationEndpoint = &regEndpoint
	go func() {
		err = d.Run()
		assert.Nil(t, err, "Run should fail for bad endpoint")
	}()

	// The server should be running now, let's check the various server types

	// Identity server
	conn, err := grpc.NewClient("unix:"+endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	assert.Nil(t, err, "Error while setting up grpc client")

	defer conn.Close() // nolint:errcheck
	idClient := csi.NewIdentityClient(conn)
	assert.NotNil(t, idClient, "Making a grpc id client failed")

	pi, err := idClient.GetPluginInfo(context.TODO(),
		&csi.GetPluginInfoRequest{})
	assert.Nil(t, err, "Error while calling GetPluginInfo")
	assert.NotNil(t, pi, "Missing PluginInfo response")
	assert.Equal(t, pi.Name, DRIVERNAME, "Name mismatch from GetPluginInfo")
	err = conn.Close()
	assert.Nil(t, err, "Error while closing identity client connection")

	// Controller server
	conn, err = grpc.NewClient("unix:"+endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	assert.Nil(t, err, "Error while setting up grpc client")

	defer conn.Close() // nolint:errcheck
	controllerClient := csi.NewControllerClient(conn)
	assert.NotNil(t, controllerClient, "Making a grpc controller client failed")

	cg, err := controllerClient.ControllerGetCapabilities(
		context.TODO(),
		&csi.ControllerGetCapabilitiesRequest{},
	)
	assert.Nil(t, err, "Error while calling ControllerGetCapabilities")
	assert.NotNil(t, cg, "Missing ControllerGetCapabilities response")
	assert.Greater(t, len(cg.Capabilities), 0, "Bad length on capabilities")
	err = conn.Close()
	assert.Nil(t, err, "Error while closing controller client connection")

	// Node server
	conn, err = grpc.NewClient("unix:"+endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	assert.Nil(t, err, "Error while setting up grpc client")

	defer conn.Close() // nolint:errcheck
	nodeClient := csi.NewNodeClient(conn)
	assert.NotNil(t, nodeClient, "Making a grpc controller client failed")

	ni, err := nodeClient.NodeGetInfo(
		context.TODO(),
		&csi.NodeGetInfoRequest{},
	)
	assert.Nil(t, err, "Error while calling NodeGetInfo")
	assert.NotNil(t, ni, "Missing NodeGetInfoResponse response")
	assert.Equal(t, ni.NodeId, nodeid, "Bad node id in NodeGetInfo response")
	err = conn.Close()
	assert.Nil(t, err, "Error while closing node client connection")

	// Stop server
	d.server.GracefulStop()
}

func TestGetPluginCapabilities(t *testing.T) {
	d := Driver{}

	r, err := d.GetPluginCapabilities(context.TODO(), &csi.GetPluginCapabilitiesRequest{})
	assert.Nil(t, err, "Unexpected error from GetPluginCapabilities")
	assert.Equal(t, 1, len(r.Capabilities), "Unexpected length of capabilities"+
		"from GetPluginCapabilities")

	v, ok := r.Capabilities[0].Type.(*csi.PluginCapability_Service_)
	assert.True(t, ok, "Can't cast as expected")

	assert.Equal(t, csi.PluginCapability_Service_CONTROLLER_SERVICE,
		v.Service.Type, "Unexpected response "+
			"from GetPluginCapabilities")
}

func TestProbe(t *testing.T) {
	d := Driver{}

	r, err := d.Probe(context.TODO(), &csi.ProbeRequest{})
	assert.Nil(t, err, "Unexpected error from Probe")
	assert.True(t, r.Ready.Value, "Unexpected response from Probe")
}

func TestDeleteVolume(t *testing.T) {
	d := Driver{}

	r, err := d.DeleteVolume(context.TODO(), &csi.DeleteVolumeRequest{})
	assert.Nil(t, err, "Unexpected error from DeleteVolume")
	assert.NotNil(t, r, "Unexpected response from DeleteVolume")
}

func TestControllerModifyVolume(t *testing.T) {
	d := Driver{}

	r, err := d.ControllerModifyVolume(
		context.TODO(),
		&csi.ControllerModifyVolumeRequest{})
	assert.Nil(t, err, "Unexpected error from ControllerModifyVolume")
	assert.NotNil(t, r, "Unexpected response from ControllerModifyVolume")
}

func TestCreateVolume(t *testing.T) {
	d := Driver{
		volumes: make(map[string]*volumeInfo),
	}

	_, err := d.CreateVolume(
		context.TODO(),
		&csi.CreateVolumeRequest{})
	assert.NotNil(t, err, "CreateVolum without capabilities should fail")

	_, err = d.CreateVolume(
		context.TODO(),
		&csi.CreateVolumeRequest{
			VolumeCapabilities: []*csi.VolumeCapability{
				&csi.VolumeCapability{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
					},
				},
			},
		})
	assert.NotNil(t, err, "CreateVolum with writer capability should fail")

	_, err = d.CreateVolume(
		context.TODO(),
		&csi.CreateVolumeRequest{
			VolumeCapabilities: []*csi.VolumeCapability{
				&csi.VolumeCapability{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
					},
				},
			},
		})
	assert.NotNil(t, err, "CreateVolume without name should fail")

	r, err := d.CreateVolume(
		context.TODO(),
		&csi.CreateVolumeRequest{
			Name: "somename",
			VolumeCapabilities: []*csi.VolumeCapability{
				&csi.VolumeCapability{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
					},
				},
			},
		})

	assert.Nil(t, err, "Unexpected error from CreateVolume")
	assert.NotNil(t, r, "Unexpected response from CreateVolume")
	assert.NotNil(t, r.Volume, "Unexpected response from CreateVolume")

	params := make(map[string]string)
	params["ignore"] = "this"

	r, err = d.CreateVolume(
		context.TODO(),
		&csi.CreateVolumeRequest{
			Name:       "somename",
			Parameters: params,
			VolumeCapabilities: []*csi.VolumeCapability{
				&csi.VolumeCapability{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
					},
				},
			},
		})

	assert.Nil(t, err, "Unexpected error from CreateVolume")
	assert.NotNil(t, r, "Unexpected response from CreateVolume")
	assert.NotNil(t, r.Volume, "Unexpected response from CreateVolume")
	assert.Equal(t, 0, len(r.Volume.GetVolumeContext()),
		"Context should be empty")

	//	Now with a valid parameter
	params["rootURL"] = "https://example.com"

	r, err = d.CreateVolume(
		context.TODO(),
		&csi.CreateVolumeRequest{
			Name:       "somename",
			Parameters: params,
			VolumeCapabilities: []*csi.VolumeCapability{
				&csi.VolumeCapability{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
					},
				},
			},
		})

	assert.Nil(t, err, "Unexpected error from CreateVolume")
	assert.NotNil(t, r, "Unexpected response from CreateVolume")
	assert.NotNil(t, r.Volume, "Unexpected response from CreateVolume")
	assert.Equal(t, 1, len(r.Volume.GetVolumeContext()),
		"Context should no longer be empty")
	assert.Contains(t, r.Volume.GetVolumeContext(), "rootURL",
		"Context should have rootURL")

}

func TestFixSocketPerms(t *testing.T) {
	d := Driver{}
	d.worldOpenSocket = false
	err := d.fixSocketPerms("something", "something")
	assert.Nil(t, err, "fixSocketPerms should not complain unless unix")

	err = d.fixSocketPerms("unix", "/does/not/exist")
	assert.Nil(t, err, "fixSocketPerms should not fail when nothing to do")

	d.worldOpenSocket = true
	err = d.fixSocketPerms("unix", "/does/not/exist")
	assert.NotNil(t, err, "fixSocketPerms should fail when "+
		"the address does not exist")

	f, err := os.CreateTemp(".", "")
	assert.Nil(t, err, "Error while making temporary file for test")
	name := f.Name()
	err = f.Close()
	assert.Nil(t, err, "Error while closing temporary file for test")

	defer os.Remove(name) // nolint:errcheck

	err = d.fixSocketPerms("unix", name)
	assert.Nil(t, err, "fixSocket should not fail for ordinary files")

	stat, err := os.Stat(name)
	assert.Nil(t, err, "Error while stating temporary file for test")

	assert.Equal(t, fs.FileMode(0o777),
		stat.Mode(),
		"fixSocketPerms should have fixed permissions")
}
