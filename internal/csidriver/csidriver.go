package csidriver

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"os/user"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"k8s.io/klog/v2"
	pluginregistration "k8s.io/kubelet/pkg/apis/pluginregistration/v1"
)

// DRIVERNAME is our name in the CSI world, should match what's in other places
const DRIVERNAME = "csi.sda.nbis.se"

// VERSIONS are the supported CSI versions
var VERSIONS = []string{"1.11.0", "1.0.0"}

// volumeInfo keeps information about a volume
type volumeInfo struct {
	Attached bool              `json:"attached"`
	Secret   string            `json:"secret"`
	ID       string            `json:"ID"`
	Path     string            `json:"path"`
	Context  map[string]string `json:"context"`
	Group    string            `json:"access_mode"`
}

// Driver is the main information bearer internally. We use a single struct for
// different "roles" of servers we offer
type Driver struct {
	csi.UnimplementedIdentityServer
	csi.UnimplementedControllerServer
	csi.UnimplementedNodeServer
	pluginregistration.UnimplementedRegistrationServer
	endpoint, nodeID, registrationEndpoint *string

	// server is used to keep track of the grpc Server if we should ever need
	// it
	server *grpc.Server

	// volumes is used to keep track of the volumes. Because of pod restarts
	// et.c. it's not authoritative and other volumes can show up and should
	// be managed properly
	volumes map[string]*volumeInfo

	// TODO: Should we just keep a reference to copy of the config here instead?

	// tokenDir determines where we should create files with the tokens
	// that are then passed to sdafs with -credentialsfile
	tokenDir *string

	// sdafsPath can specify to use a specific path to execute sdafs
	sdafsPath *string

	// logDir if set points to where we want to store logs
	logDir *string

	// persistDir if set points to where to read/store persistence information
	// for recovering mounts on pod restart
	persistDir *string

	// myUid keeps track of the user id we're running as
	myUid int

	// worldOpenSocket signals whatever the sockets we create should have
	// permissions allowing it to be used by only our user (myUid) if false
	// or everybody if true
	worldOpenSocket bool

	// these functions are managed as struct fields to simplify testing

	// mounter is the function to be used to mount a volume (doMount)
	mounter func(*Driver, *volumeInfo) error

	// unmounter is the function to be used to unmount a volume (unmount)
	unmounter func(*Driver, *volumeInfo) error

	// writeToken is used to write out the token to use for a volume
	writeToken func(*Driver, *volumeInfo) error

	// isMountPoint reports the requested mounting point of the volume is a
	// mount point
	isMountPoint func(*Driver, *volumeInfo) bool

	// maxWaitMount determines how long we wait for a mount to show up
	// before reporting a failure
	maxWaitMount time.Duration
	// waitPeriod determines how long we wait between each check for whatever
	// a mount has shown up
	waitPeriod time.Duration
}

// CSIConfig is the incomning configuration for the driver
type CSIConfig struct {
	Endpoint             *string
	NodeID               *string
	RegistrationEndpoint *string

	// TokenDir corresponds to tokenDir in Driver
	TokenDir *string

	// LogDir corresponds to logDir in Driver
	LogDir *string

	// SdafsPath corresponds to sdafsPath in Driver
	SdafsPath *string

	// WorldOpen signals whatever we should make the socket we create world
	// accessible or not
	WorldOpen *bool

	// PersistDir says where to persist mount information for recovering on pod
	// restarts (if we should)
	PersistDir *string
}

// registerKubelet registers the driver within kubelet
func (d *Driver) registerKubelet() error {

	klog.V(8).Infof("Registering CSI driver with kubelet")

	csiServer := makeGrpcServer("kubelet registration")

	registrationEndpointPath := "unix:///var/lib/kubelet/plugins/kubelet.sock"
	if d.registrationEndpoint != nil {
		registrationEndpointPath = *d.registrationEndpoint
	}

	network, address := endpointToNetworkAddress(registrationEndpointPath)
	listener, err := net.Listen(network, address)

	if err != nil {
		return fmt.Errorf("error while setting up listen for grpc: %v", err)
	}

	err = d.fixSocketPerms(network, address)
	if err != nil {
		return fmt.Errorf("error while making socket accessible: %v", err)
	}

	pluginregistration.RegisterRegistrationServer(csiServer, d)

	go func() {
		defer listener.Close() // nolint:errcheck

		err := csiServer.Serve(listener)
		if err != nil {
			klog.Errorf("serving of registration GRPC failed: %v", err)
		}
	}()

	return nil
}

// NewDriver returns a Driver object. Since the volumes map should be
// initialised NewDriver should be used.
func NewDriver(config *CSIConfig) (*Driver, error) {
	for _, path := range []*string{config.Endpoint,
		config.RegistrationEndpoint} {

		if path == nil {
			return nil, fmt.Errorf("missing socket configuration")
		}

		cont, err := checkSocket(path)
		if !cont && err == nil {
			return nil, fmt.Errorf("aborting since something responds on %s",
				*path)
		}

		if err != nil {
			return nil, fmt.Errorf("problem with socket path %s: %v",
				*path, err)
		}
	}

	currentUser, err := user.Current()

	if err != nil {
		return nil, fmt.Errorf("don't even know who I am: %v", err)
	}

	uid, err := strconv.Atoi(currentUser.Uid)
	if err != nil {
		return nil, fmt.Errorf("converting Uid string %s failed: %v",
			currentUser.Uid, err)
	}

	return &Driver{
		endpoint:             config.Endpoint,
		nodeID:               config.NodeID,
		registrationEndpoint: config.RegistrationEndpoint,
		volumes:              make(map[string]*volumeInfo),
		tokenDir:             config.TokenDir,
		sdafsPath:            config.SdafsPath,
		logDir:               config.LogDir,
		persistDir:           config.PersistDir,
		myUid:                uid,
		worldOpenSocket:      *config.WorldOpen,
		mounter:              doMount,
		unmounter:            unmount,
		writeToken:           writeToken,
		isMountPoint:         isMountPoint,
		waitPeriod:           10 * time.Millisecond,
		maxWaitMount:         60 * time.Second,
	}, nil
}

// endpointToNetworkAddress converts a string, possibly with a protocol
// qualifier to a string tuple (protocol, address)
func endpointToNetworkAddress(s string) (string, string) {

	endpointParts := strings.Split(s, ":")
	if len(endpointParts) == 1 {
		return "unix", s
	}

	return endpointParts[0], endpointParts[1]
}

// makeGrpcServer creates a grpc server that does call logging
func makeGrpcServer(serverName string) *grpc.Server {
	opts := []grpc.ServerOption{grpc.UnaryInterceptor(
		func(ctx context.Context, req interface{},
			_ *grpc.UnaryServerInfo,
			handler grpc.UnaryHandler) (resp interface{}, err error) {
			klog.V(10).Infof("Call to %s, request type %T", serverName, req)

			logger := klog.FromContext(ctx)
			resp, err = handler(klog.NewContext(ctx, logger), req)
			if err != nil {
				klog.V(8).Infof("Responded with error: %v", err)
			}
			return resp, err
		}),
	}

	return grpc.NewServer(opts...)
}

// fixSocketPerms ensures the given socket has the desired permissions
func (d *Driver) fixSocketPerms(network, address string) error {
	if !d.worldOpenSocket || network != "unix" {
		return nil
	}

	// Full world access requested and should be acted on
	err := os.Chmod(address, 0o0777)
	if err != nil {
		return fmt.Errorf("can't make socket accessible: %v", err)
	}

	return nil
}

// Run setups and launches the main grpc server
func (d *Driver) Run() error {
	klog.V(4).Infof("Starting CSI")

	d.server = makeGrpcServer("CSI socket")

	network, address := endpointToNetworkAddress(*d.endpoint)
	listener, err := net.Listen(network, address)

	if err != nil {
		return fmt.Errorf("error while setting up listen for grpc: %v", err)
	}

	err = d.fixSocketPerms(network, address)
	if err != nil {
		return fmt.Errorf("error while fixing sockets permissions: %v", err)
	}

	// Close (remove) when we're done
	defer os.Remove(address) // nolint:errcheck
	defer listener.Close()   // nolint:errcheck

	if d.persistDir != nil && len(*d.persistDir) > 0 {
		err = d.loadPersisted()
		if err != nil {
			return fmt.Errorf("error while loading persisted mountes: %v", err)
		}

	}

	klog.V(4).Infof("Registering with kubelet")

	err = d.registerKubelet()
	if err != nil {
		return fmt.Errorf("error while registering with kubelet: %v", err)
	}

	klog.V(4).Infof("RegisterIdentityServer")
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

// handleSignals waits for signals and closes down/cleans up when received
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

		if l.Addr().Network() != "unix" {
			os.Exit(0)
		}

		err = os.Remove(l.Addr().String())
		if err != nil {
			klog.Errorf("Removing socket file %s failed: %v",
				l.Addr().String(),
				err)
			os.Exit(1)
		}

		os.Exit(0)
	}
}

// GetPluginCapabilities returns the capabilities of the service
func (d *Driver) GetPluginCapabilities(_ context.Context,
	r *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse,
	error) {

	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			}},
	}, nil
}

// GetPluginInfo returns the plugin information
func (d *Driver) GetPluginInfo(_ context.Context,
	r *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {

	return &csi.GetPluginInfoResponse{
		Name:          DRIVERNAME,
		VendorVersion: "0.0.1",
	}, nil
}

// Probe returns whatever the service is ready
func (d *Driver) Probe(_ context.Context, r *csi.ProbeRequest) (*csi.ProbeResponse, error) {

	return &csi.ProbeResponse{
		Ready: wrapperspb.Bool(true),
	}, nil
}

// socketNameCleanup removes any initial unix: and makes the path look nicer
func socketNameCleanup(s string) string {
	return path.Clean(strings.TrimPrefix(s, "unix:"))
}

// GetInfo is the RPC invoked by plugin watcher
func (d *Driver) GetInfo(_ context.Context,
	r *pluginregistration.InfoRequest) (*pluginregistration.PluginInfo, error) {

	responsePluginInfo := pluginregistration.PluginInfo{
		Type:              pluginregistration.CSIPlugin,
		Name:              DRIVERNAME,
		Endpoint:          socketNameCleanup(*d.endpoint),
		SupportedVersions: VERSIONS,
	}

	return &responsePluginInfo, nil
}

// NotifyRegistrationStatus is called when we are registered
func (d *Driver) NotifyRegistrationStatus(_ context.Context,
	r *pluginregistration.RegistrationStatus) (*pluginregistration.RegistrationStatusResponse, error) {

	if !r.PluginRegistered {
		klog.Error(fmt.Errorf("registration process failed, hoping for retry. The error was :%v", r.Error))
	}

	return &pluginregistration.RegistrationStatusResponse{}, nil
}

// getControllerCapabilities is a helper function to generate a suitable
// capability list
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

// ControllerGetCapabilities is called through grpc and responds with our
// capabilities
func (d *Driver) ControllerGetCapabilities(_ context.Context,
	r *csi.ControllerGetCapabilitiesRequest) (
	*csi.ControllerGetCapabilitiesResponse, error) {
	resp := &csi.ControllerGetCapabilitiesResponse{
		Capabilities: getControllerCapabilites()}

	return resp, nil
}

// CreateVolume is called through grpc to go through the CSI parts to create
// a volume
func (d *Driver) CreateVolume(_ context.Context,
	r *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {

	caps := r.GetVolumeCapabilities()
	if caps == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume Capabilities missing in request")
	}

	mountGroup := ""

	for _, cap := range caps {
		am := cap.GetAccessMode()

		if am != nil && am.GetMode() != csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY &&
			am.GetMode() != csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY {
			klog.V(10).Infof("Unsupported requested capability %v", cap.AccessMode)
			return nil, status.Error(codes.PermissionDenied, "Only read-only is supported")
		}

		mount := cap.GetMount()
		if mount != nil && mount.GetVolumeMountGroup() != "" {
			mountGroup = mount.GetVolumeMountGroup()
		}
	}

	name := r.GetName()

	if len(name) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Invalid name in request")
	}

	context := make(map[string]string)

	// Add some defaults
	// Note: sdafs defaults to no TTL-based expiry but for CSI usage we default
	// to keeping not very long
	context["cachettl"] = "120s"

	params := r.GetParameters()

	if params != nil {

		klog.V(14).Infof("Parameters for volume creation are %v", r.GetParameters())

		possibleParameters := []string{"chunksize", "rootURL", "cachesize",
			"maxretries", "tokenkey", "extraca", "owner", "group", "cachettl"}
		for _, p := range possibleParameters {
			value, found := params[p]
			if found {
				context[p] = value
			}
		}
	} else {
		klog.V(14).Infof("No parameters for volume")
	}

	if mountGroup != "" {
		// CSI has precedence over specific options passed
		context["group"] = mountGroup
	}

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      name,
			CapacityBytes: 0,
			VolumeContext: context,
		},
	}, nil
}

// DeleteVolume is called through grpc to initiate removal of a volume
// TODO: Should we go through the work to check if the volume is mounted
// and fail if that's the case. We don't want to persist things and I think
// kubernetes should manage that iself
func (d *Driver) DeleteVolume(_ context.Context, r *csi.DeleteVolumeRequest) (
	*csi.DeleteVolumeResponse, error) {
	klog.V(10).Infof("Delete request for %s", r.GetVolumeId())

	return &csi.DeleteVolumeResponse{}, nil
}

// ControllerModifyVolume is called through grpc upon changes, currently
// not implemented
// TODO: Can we get notified on updates to the secret?
func (d *Driver) ControllerModifyVolume(_ context.Context,
	r *csi.ControllerModifyVolumeRequest) (
	*csi.ControllerModifyVolumeResponse, error) {

	return &csi.ControllerModifyVolumeResponse{}, nil
}
