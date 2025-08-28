package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/NBISweden/sdafs/internal/csidriver"
	"k8s.io/klog/v2"
)

func usage() {
	_, err := fmt.Fprintf(flag.CommandLine.Output(),
		"Usage: %s [FLAGS...] mountpoint\n\nSupported flags are:\n\n",
		os.Args[0])
	if err != nil {
		panic("the world is unreliable, we can't go on")
	}
	flag.PrintDefaults()
	os.Exit(0)
}

func sdafsPathDefault() string {
	c, err := exec.LookPath("sdafs")

	if err != nil || len(c) == 0 {
		return "/sdafs"
	}

	return c
}

func main() {
	klog.InitFlags(nil)

	endpointDefault := os.Getenv("CSI_ENDPOINT")
	if endpointDefault == "" {
		endpointDefault = "unix:///var/lib/kubelet/plugins/csi.sda.nbis.se/csi.sock"
	}

	nodeIDDefault := os.Getenv("CSI_NODE_ID")
	if nodeIDDefault == "" {
		nodeIDDefault = "nodeid"
	}

	registrationEndpointDefault := "unix:///var/lib/kubelet/plugins_registry/csi.sda.nbis.se.reg.sock"

	help := flag.Bool("help", false, "Show usage")

	endpoint := flag.String("csi-address", endpointDefault, "CSI Endpoint")
	nodeID := flag.String("node-id", nodeIDDefault,
		"node-id to report in NodeGetInfo RPC")
	registrationEndpoint := flag.String("registrationendpoint", registrationEndpointDefault,
		"Kubelet device plugin registration socket")

	tokenDir := flag.String("tokendir", "/tmp", "Where to store temporary files for tokens")
	logDir := flag.String("sdafslogdir", "/tmp", "Where to create logfiles for sdafs, none if empty")

	sdafsPath := flag.String("sdafspath", sdafsPathDefault(), "Path to call sdafs")

	flag.Parse()

	if *help {
		usage()
	}

	klog.V(3).Infof(
		"Configuration: CSI Endpoint: %s, NodeId: %s, Kubelet socket: %s",
		*endpoint, *nodeID, *registrationEndpoint)

	cont, err := csidriver.CheckSocket(endpoint)
	if !cont && err != nil {
		klog.Fatalf("Problem with socket path %s: %v", *endpoint, err)
	}

	cont, err = csidriver.CheckSocket(registrationEndpoint)
	if !cont && err != nil {
		klog.Fatalf("Problem with socket path %s: %v", *registrationEndpoint, err)
	}

	d, err := csidriver.NewDriver(endpoint, nodeID, registrationEndpoint, tokenDir,
		sdafsPath, logDir)
	if err != nil {
		klog.Fatalf("Failed setting up driver: %v", err)
	}

	err = d.Run()
	klog.V(0).Infof("CSI driver run failed: %v", err)

}
