package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/NBISweden/sdafs/internal/csidriver"
	"k8s.io/klog/v2"
)

func usage(exit int) {
	_, err := fmt.Fprintf(flag.CommandLine.Output(),
		"Usage: %s [FLAGS...]\n\nSupported flags are:\n\n",
		os.Args[0])
	if err != nil {
		panic("the world is unreliable, we can't go on")
	}
	flag.PrintDefaults()
	os.Exit(exit)
}

func sdafsPathDefault() string {
	c, err := exec.LookPath("sdafs")

	if err != nil || len(c) == 0 {
		return "/sdafs"
	}

	return c
}

func getConfig() *csidriver.CSIConfig {
	m := csidriver.CSIConfig{}
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

	m.Endpoint = flag.String("csi-address", endpointDefault, "CSI Endpoint")
	m.NodeID = flag.String("node-id", nodeIDDefault,
		"node-id to report in NodeGetInfo RPC")
	m.RegistrationEndpoint = flag.String("registrationendpoint", registrationEndpointDefault,
		"Kubelet device plugin registration socket")

	m.TokenDir = flag.String("tokendir", "/tmp", "Where to store temporary files for tokens")
	m.LogDir = flag.String("sdafslogdir", "/tmp", "Where to create logfiles for sdafs, none if empty")

	m.SdafsPath = flag.String("sdafspath", sdafsPathDefault(), "Path to call sdafs")

	m.WorldOpen = flag.Bool("world-open-socket", false, "Make sockets created world accessible")

	flag.Parse()

	if *help {
		usage(0)
	}

	klog.V(3).Infof(
		"Configuration: CSI Endpoint: %s, NodeId: %s, Kubelet socket: %s",
		*m.Endpoint, *m.NodeID, *m.RegistrationEndpoint)

	return &m
}

func main() {
	klog.InitFlags(nil)

	config := getConfig()

	d, err := csidriver.NewDriver(config)
	if err != nil {
		klog.Fatalf("Failed setting up sdafs CSI driver: %v", err)
	}

	err = d.Run()
	klog.V(0).Infof("CSI driver run failed: %v", err)

}
