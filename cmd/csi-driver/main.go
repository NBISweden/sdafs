package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/NBISweden/sdafs/internal/csidriver"
	"k8s.io/klog/v2"
)

func usage() {
	fmt.Fprintf(flag.CommandLine.Output(),
		"Usage: %s [FLAGS...] mountpoint\n\nSupported flags are:\n\n",
		os.Args[0])
	flag.PrintDefaults()
	os.Exit(0)
}

func main() {
	klog.InitFlags(nil)

	endpointDefault := os.Getenv("CSI_ENDPOINT")
	if endpointDefault == "" {
		endpointDefault = "unix://tmp/csi.sock"
	}

	nodeIDDefault := os.Getenv("CSI_NODE_ID")
	if nodeIDDefault == "" {
		nodeIDDefault = "nodeid"
	}

	kubeletEndpointDefault := "unix:///var/lib/kubelet/device-plugin/kubelet.sock"

	help := flag.Bool("help", false, "Show usage")

	endpoint := flag.String("endpoint", endpointDefault, "CSI Endpoint")
	nodeID := flag.String("node-id", nodeIDDefault,
		"node-id to report in NodeGetInfo RPC")
	kubeletEndpoint := flag.String("kubeletendpoint", kubeletEndpointDefault,
		"Kubelet device plugin registration socket")

	flag.Parse()

	if *help {
		usage()
	}

	klog.V(3).Infof(
		"Configuration: CSI Endpoint: %s, NodeId: %s, Kubelet socket: %s",
		*endpoint, *nodeID, *kubeletEndpoint)

	d := csidriver.NewDriver(endpoint, nodeID, kubeletEndpoint)
	err := d.Run()
	klog.V(0).Infof("CSI driver run failed: %v", err)

}
