package csidriver

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"k8s.io/klog/v2"
	pluginregistration "k8s.io/kubelet/pkg/apis/pluginregistration/v1"
)

const DRIVERNAME = "csi.sda.nbis.se"

var VERSIONS = []string{"1.31.0"}

func (d *Driver) registerKubelet() error {

	// kubeletSocketPath := "unix:///var/lib/kubelet/plugins/kubelet.sock"
	// if d.kubeletSocket != nil {
	// 	kubeletSocketPath = *d.kubeletSocket
	// }

	// if err != nil {
	// 	return fmt.Errorf("Couldn't make kubelet grpc connection: %v", err)
	// }

	// klog.V(4).Infof("%v err %v", c, err)

	// pr := pluginregistration.NewRegistrationClient(c)
	// pr.GetInfo()
	// pluginregistration.RegisterRegistrationServer()

	return nil
}

type Driver struct {
	csi.UnimplementedIdentityServer
	csi.UnimplementedControllerServer
	csi.UnimplementedNodeServer
	endpoint, nodeId, kubeletSocket *string
	server                          *grpc.Server
}

func NewDriver(endpoint, nodeId, kubeletSocket *string) *Driver {
	return &Driver{
		endpoint:      endpoint,
		nodeId:        nodeId,
		kubeletSocket: kubeletSocket,
	}
}

func (d *Driver) Run() error {
	opts := []grpc.ServerOption{}
	d.server = grpc.NewServer(opts...)

	endpointParts := strings.Split(*d.endpoint, ":")
	listener, err := net.Listen(endpointParts[0], endpointParts[1])

	if err != nil {
		return fmt.Errorf("Error while setting up listen for grpc: %v", err)
	}

	// Close (remove) when we're done
	defer listener.Close()

	klog.V(4).Infof("Registering")

	// err = d.registerKubelet()
	// if err != nil {
	// 	return fmt.Errorf("Error while registering with kubelet: %v", err)
	// }

	csi.RegisterIdentityServer(d.server, d)
	csi.RegisterControllerServer(d.server, d)
	csi.RegisterNodeServer(d.server, d)

	ch := make(chan os.Signal, 1)
	go handleSignals(ch, listener)
	signal.Notify(ch, os.Interrupt)

	err = d.server.Serve(listener)
	if err != nil {
		klog.Errorf("Serving stopped with error %v", err)
		return fmt.Errorf("Serving failed: %v", err)
	}

	return nil
}

func handleSignals(c chan os.Signal, l net.Listener) {
	for {
		s := <-c

		klog.V(1).Infof("Received signal %v, exiting", s)
		err := l.Close()
		if err != nil {
			klog.Errorf("Closing listening socket failed: %v", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
}

// Identity interface implementation
func (d *Driver) GetPluginCapabilities(c context.Context, r *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	klog.V(10).Infof("GetPluginCapabilities: request %v", r)
	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{},
	}, nil
}

func (d *Driver) GetPluginInfo(c context.Context, r *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	klog.V(10).Infof("GetPluginInfo: request %v", r)
	return &csi.GetPluginInfoResponse{
		Name:          "sdafs-csi",
		VendorVersion: "0.0.1",
	}, nil
}

func (d *Driver) driverReady() bool {

	return true
}

func (d *Driver) Probe(c context.Context, r *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	klog.V(10).Infof("Probe: request %v", r)
	return &csi.ProbeResponse{
		Ready: wrapperspb.Bool(d.driverReady()),
	}, nil
}

func (d *Driver) NodePublishVolume(c context.Context, r *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	klog.V(10).Infof("NodePublishVolume: request %v", r)

	return &csi.NodePublishVolumeResponse{}, nil
}

func (d *Driver) NodeUnpublishVolume(c context.Context, r *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	klog.V(10).Infof("NodeUnpublishVolume: request %v", r)

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (d *Driver) NodeGetInfo(c context.Context, r *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	klog.V(10).Infof("NodeGetInfo: request %v", r)

	return &csi.NodeGetInfoResponse{
		NodeId: *d.nodeId,
	}, nil
}

func (d *Driver) NodeGetCapabilities(c context.Context, r *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	klog.V(10).Infof("NodeGetCapabilities: request %v", r)

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{},
	}, nil
}

// GetInfo is the RPC invoked by plugin watcher
func (d *Driver) GetInfo(ctx context.Context, r *pluginregistration.InfoRequest) (*pluginregistration.PluginInfo, error) {
	klog.V(10).Infof("GetInfo: request %v", r)

	return &pluginregistration.PluginInfo{
		Type:              pluginregistration.CSIPlugin,
		Name:              DRIVERNAME,
		Endpoint:          *d.endpoint,
		SupportedVersions: VERSIONS,
	}, nil
}

func (d *Driver) NotifyRegistrationStatus(c context.Context, r *pluginregistration.RegistrationStatus) (*pluginregistration.RegistrationStatusResponse, error) {
	klog.V(10).Infof("GetInfo: request %v", r)

	if !r.PluginRegistered {
		klog.Error(fmt.Errorf("Registration process failed, bailing out. The error was :%v", r.Error))
		os.Exit(1)
	}

	return &pluginregistration.RegistrationStatusResponse{}, nil
}
