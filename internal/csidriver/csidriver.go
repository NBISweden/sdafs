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

var VERSIONS = []string{"1.0.0"}

// registerKubelet registers the driver within kubelet
func (d *Driver) registerKubelet() error {

	klog.V(8).Infof("Registering CSI driver with kubelet")

	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(
			func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
				klog.V(10).Infof("Call to kubelet registration socket, request: %T %v", req, req)

				logger := klog.FromContext(ctx)
				resp, err = handler(klog.NewContext(ctx, logger), req)
				klog.V(10).Infof("Response,err: %v, %v", resp, err)
				return resp, err
			}),
	}
	csiServer := grpc.NewServer(opts...)

	kubeletSocketPath := "unix:///var/lib/kubelet/plugins/kubelet.sock"
	if d.kubeletSocket != nil {
		kubeletSocketPath = *d.kubeletSocket
	}

	endpointParts := strings.Split(kubeletSocketPath, ":")
	listener, err := net.Listen(endpointParts[0], endpointParts[1])

	if err != nil {
		return fmt.Errorf("error while setting up listen for grpc: %v", err)
	}

	pluginregistration.RegisterRegistrationServer(csiServer, d)

	go func() {
		err := csiServer.Serve(listener)
		if err != nil {
			klog.Errorf("serving of registration GRPC failed: %v", err)
		}
	}()

	return nil
}

type Driver struct {
	csi.UnimplementedIdentityServer
	csi.UnimplementedControllerServer
	csi.UnimplementedNodeServer
	endpoint, nodeID, kubeletSocket *string
	server                          *grpc.Server
}

func NewDriver(endpoint, nodeID, kubeletSocket *string) *Driver {
	return &Driver{
		endpoint:      endpoint,
		nodeID:        nodeID,
		kubeletSocket: kubeletSocket,
	}
}

func (d *Driver) Run() error {
	klog.V(4).Infof("Starting CSI")

	opts := []grpc.ServerOption{grpc.UnaryInterceptor(func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		klog.V(10).Infof("Call to CSI socket, request: %T %v", req, req)

		logger := klog.FromContext(ctx)
		resp, err = handler(klog.NewContext(ctx, logger), req)
		klog.V(10).Infof("Response,err: %v, %v", resp, err)
		return resp, err
	}),
	}
	d.server = grpc.NewServer(opts...)

	endpointParts := strings.Split(*d.endpoint, ":")
	listener, err := net.Listen(endpointParts[0], endpointParts[1])

	if err != nil {
		return fmt.Errorf("error while setting up listen for grpc: %v", err)
	}

	// Close (remove) when we're done
	defer listener.Close() // nolint:errcheck

	klog.V(4).Infof("Registering")

	err = d.registerKubelet()
	if err != nil {
		return fmt.Errorf("error while registering with kubelet: %v", err)
	}

	klog.V(4).Infof("RegisterIdentiyServer")

	csi.RegisterIdentityServer(d.server, d)
	klog.V(4).Infof("RegisterControllerServer")
	csi.RegisterControllerServer(d.server, d)
	klog.V(4).Infof("RegisterNodeServer")
	csi.RegisterNodeServer(d.server, d)

	klog.V(4).Infof("Starting normal server")

	ch := make(chan os.Signal, 1)
	go handleSignals(ch, listener)
	signal.Notify(ch, os.Interrupt)

	klog.V(4).Infof("Starting CSI grpc server")

	err = d.server.Serve(listener)
	if err != nil {
		klog.Errorf("Serving stopped with error %v", err)
		return fmt.Errorf("serving failed: %v", err)
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
func (d *Driver) GetPluginCapabilities(_ context.Context, r *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	klog.V(10).Infof("GetPluginCapabilities: request %v", r)
	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{},
	}, nil
}

func (d *Driver) GetPluginInfo(_ context.Context, r *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	klog.V(10).Infof("GetPluginInfo: request %v", r)
	return &csi.GetPluginInfoResponse{
		Name:          "sdafs-csi",
		VendorVersion: "0.0.1",
	}, nil
}

func (d *Driver) driverReady() bool {

	return true
}

func (d *Driver) Probe(_ context.Context, r *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	klog.V(10).Infof("Probe: request %v", r)
	return &csi.ProbeResponse{
		Ready: wrapperspb.Bool(d.driverReady()),
	}, nil
}

func (d *Driver) NodePublishVolume(_ context.Context, r *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	klog.V(10).Infof("NodePublishVolume: request %v", r)

	return &csi.NodePublishVolumeResponse{}, nil
}

func (d *Driver) NodeUnpublishVolume(_ context.Context, r *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	klog.V(10).Infof("NodeUnpublishVolume: request %v", r)

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (d *Driver) NodeGetInfo(_ context.Context, r *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	klog.V(10).Infof("NodeGetInfo: request %v", r)

	return &csi.NodeGetInfoResponse{
		NodeId:            *d.nodeID,
		MaxVolumesPerNode: 100000,
	}, nil
}

func (d *Driver) NodeGetCapabilities(_ context.Context, r *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	klog.V(10).Infof("NodeGetCapabilities: request %v", r)

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{},
	}, nil
}

// socketCleanup removes any initial unix://
func socketCleanup(s string) string {
	return strings.TrimPrefix(s, "unix:/")
}

// GetInfo is the RPC invoked by plugin watcher
func (d *Driver) GetInfo(_ context.Context, r *pluginregistration.InfoRequest) (*pluginregistration.PluginInfo, error) {
	klog.V(10).Infof("GetInfo: request %v", r)

	responsePluginInfo := pluginregistration.PluginInfo{
		Type:              pluginregistration.CSIPlugin,
		Name:              DRIVERNAME,
		Endpoint:          socketCleanup(*d.endpoint), //*d.endpoint,
		SupportedVersions: VERSIONS,
	}

	klog.V(10).Infof("GetInfo: response %v", responsePluginInfo)

	return &responsePluginInfo, nil
}

func (d *Driver) NotifyRegistrationStatus(_ context.Context, r *pluginregistration.RegistrationStatus) (*pluginregistration.RegistrationStatusResponse, error) {
	klog.V(10).Infof("GetInfo: request %v", r)

	if !r.PluginRegistered {
		klog.Error(fmt.Errorf("registration process failed, bailing out. The error was :%v", r.Error))
		os.Exit(1)
	}

	return &pluginregistration.RegistrationStatusResponse{}, nil
}
