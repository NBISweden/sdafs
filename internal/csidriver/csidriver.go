package csidriver

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
				klog.V(10).Infof("Call on kubelet registration socket, request: %T", req)

				logger := klog.FromContext(ctx)
				resp, err = handler(klog.NewContext(ctx, logger), req)

				return resp, err
			}),
	}
	csiServer := grpc.NewServer(opts...)

	kubeletSocketPath := "unix:///var/lib/kubelet/plugins/kubelet.sock"
	if d.kubeletSocket != nil {
		kubeletSocketPath = *d.kubeletSocket
	}

	var network, address string
	network = "unix"
	address = kubeletSocketPath

	endpointParts := strings.Split(kubeletSocketPath, ":")
	if len(endpointParts) > 1 {
		network = endpointParts[0]
		address = endpointParts[1]
	}
	listener, err := net.Listen(network, address)

	if err != nil {
		return fmt.Errorf("error while setting up listen for grpc: %v", err)
	}

	pluginregistration.RegisterRegistrationServer(csiServer, d)

	go func() {
		defer listener.Close()

		err := csiServer.Serve(listener)
		if err != nil {
			klog.Errorf("serving of registration GRPC failed: %v", err)
		}
	}()

	return nil
}

// volumeInfo keeps
type volumeInfo struct {
	attached bool
	pid      int
	secret   string
	ID       string
	path     string
}

// Driver is the main information bearer. We use a single struct for different
// "roles" of servers we offer
type Driver struct {
	csi.UnimplementedIdentityServer
	csi.UnimplementedControllerServer
	csi.UnimplementedNodeServer
	endpoint, nodeID, kubeletSocket *string
	server                          *grpc.Server
	volumes                         map[string]*volumeInfo
	tokenDir                        *string
	sdafsPath                       *string
	logDir                          *string
}

// NewDriver returns a Driver object. Since the volumes map should be
// initiaalised NewDriver should be used.
func NewDriver(endpoint, nodeID, kubeletSocket, tokenDir, sdafsPath, logDir *string) *Driver {
	return &Driver{
		endpoint:      endpoint,
		nodeID:        nodeID,
		kubeletSocket: kubeletSocket,
		volumes:       make(map[string]*volumeInfo),
		tokenDir:      tokenDir,
		sdafsPath:     sdafsPath,
		logDir:        logDir,
	}
}

func (d *Driver) Run() error {
	klog.V(4).Infof("Starting CSI")

	opts := []grpc.ServerOption{grpc.UnaryInterceptor(func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		klog.V(10).Infof("Call to CSI socket, request type %T", req)

		logger := klog.FromContext(ctx)
		resp, err = handler(klog.NewContext(ctx, logger), req)
		if err != nil {
			klog.V(8).Infof("Responded with error: %v", err)
		}
		return resp, err
	}),
	}
	d.server = grpc.NewServer(opts...)

	var network, address string

	endpointParts := strings.Split(*d.endpoint, ":")
	if len(endpointParts) > 1 {
		network = endpointParts[0]
		address = endpointParts[1]
	} else {
		network = "unix"
		address = endpointParts[0]
	}

	listener, err := net.Listen(network, address)

	if err != nil {
		return fmt.Errorf("error while setting up listen for grpc: %v", err)
	}

	// Close (remove) when we're done
	defer os.Remove(address)
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
	signal.Notify(ch, syscall.SIGTERM)

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

		// Remove socket

		if l.Addr().Network() == "unix" {
			os.Remove(l.Addr().String())
		}

		os.Exit(0)
	}
}

// Identity interface implementation
func (d *Driver) GetPluginCapabilities(_ context.Context, r *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {

	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			&csi.PluginCapability{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			}},
	}, nil
}

func (d *Driver) GetPluginInfo(_ context.Context, r *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {

	return &csi.GetPluginInfoResponse{
		Name:          "csi.sda.nbis.se",
		VendorVersion: "0.0.1",
	}, nil
}

func (d *Driver) driverReady() bool {
	return true
}

func (d *Driver) Probe(_ context.Context, r *csi.ProbeRequest) (*csi.ProbeResponse, error) {

	return &csi.ProbeResponse{
		Ready: wrapperspb.Bool(d.driverReady()),
	}, nil
}

// socketCleanup removes any initial unix:
func socketCleanup(s string) string {
	return path.Clean(strings.TrimPrefix(s, "unix:/"))
}

// GetInfo is the RPC invoked by plugin watcher
func (d *Driver) GetInfo(_ context.Context, r *pluginregistration.InfoRequest) (*pluginregistration.PluginInfo, error) {

	responsePluginInfo := pluginregistration.PluginInfo{
		Type:              pluginregistration.CSIPlugin,
		Name:              DRIVERNAME,
		Endpoint:          socketCleanup(*d.endpoint),
		SupportedVersions: VERSIONS,
	}

	return &responsePluginInfo, nil
}

func (d *Driver) NotifyRegistrationStatus(_ context.Context, r *pluginregistration.RegistrationStatus) (*pluginregistration.RegistrationStatusResponse, error) {

	if !r.PluginRegistered {
		klog.Error(fmt.Errorf("registration process failed, hoping for retry. The error was :%v", r.Error))
	}

	return &pluginregistration.RegistrationStatusResponse{}, nil
}

func getControllerCapabilites() (c []*csi.ControllerServiceCapability) {

	for _, capability := range []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_MODIFY_VOLUME,
	} {

		c = append(c, &csi.ControllerServiceCapability{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: capability,
				},
			},
		})
	}

	return c
}

func (d *Driver) ControllerGetCapabilities(_ context.Context, r *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {

	resp := &csi.ControllerGetCapabilitiesResponse{
		Capabilities: getControllerCapabilites()}

	klog.V(20).Infof("ControllerGetCapabilitiesetInfo: responding %v", *resp)

	return resp, nil
}

func (d *Driver) CreateVolume(_ context.Context, r *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {

	caps := r.GetVolumeCapabilities()
	if caps == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume Capabilities missing in request")
	}

	for _, cap := range caps {
		am := cap.GetAccessMode()

		if am.GetMode() != csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY &&
			am.GetMode() != csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY {
			klog.V(10).Infof("Unsupported requested capability %v", cap.AccessMode)
			return nil, status.Error(codes.PermissionDenied, "Only read-only is supported")

		}
	}

	name := r.GetName()

	if len(name) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Invalid name in request")
	}

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      name,
			CapacityBytes: 0,
			VolumeContext: nil,
		},
	}, nil

}

func (d *Driver) DeleteVolume(_ context.Context, r *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {

	klog.V(10).Infof("Delete request for %s", r.GetVolumeId())

	return &csi.DeleteVolumeResponse{}, nil
}

func (d *Driver) ControllerModifyVolume(_ context.Context, r *csi.ControllerModifyVolumeRequest) (*csi.ControllerModifyVolumeResponse, error) {

	return &csi.ControllerModifyVolumeResponse{}, nil
}
