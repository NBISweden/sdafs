package main

import (
	"flag"
	"fmt"
	"os"

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

	endpoint := flag.String("driverendpoint", endpointDefault, "CSI Endpoint")
	nodeID := flag.String("node-id", nodeIDDefault,
		"node-id to report in NodeGetInfo RPC")
	registrationEndpoint := flag.String("registrationendpoint", registrationEndpointDefault,
		"Kubelet device plugin registration socket")

	flag.Parse()

	if *help {
		usage()
	}

	klog.V(3).Infof(
		"Configuration: CSI Endpoint: %s, NodeId: %s, Kubelet socket: %s",
		*endpoint, *nodeID, *registrationEndpoint)

	d := csidriver.NewDriver(endpoint, nodeID, registrationEndpoint)
	err := d.Run()
	klog.V(0).Infof("CSI driver run failed: %v", err)

}
