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
	os.Exit(1)
}

func main() {
	klog.InitFlags(nil)

	endpointDefault := os.Getenv("CSI_ENDPOINT")
	if endpointDefault == "" {
		endpointDefault = "unix://tmp/csi.sock"
	}

	nodeIdDefault := os.Getenv("CSI_NODE_ID")
	if nodeIdDefault == "" {
		nodeIdDefault = "nodeid"
	}

	kubeletEndpointDefault := "unix:///var/lib/kubelet/device-plugin/kubelet.sock"

	endpoint := flag.String("endpoint", endpointDefault, "CSI Endpoint")
	nodeId := flag.String("node-id", nodeIdDefault,
		"node-id to report in NodeGetInfo RPC")
	kubeletEndpoint := flag.String("kubeletendpoint", kubeletEndpointDefault,
		"Kubelet device plugin registration socket")

	flag.Parse()

	klog.V(3).Infof(
		"Configuration: CSI Endpoint: %s, NodeId: %s, Kubelet socket: %s",
		*endpoint, *nodeId, *kubeletEndpoint)

	d := csidriver.NewDriver(endpoint, nodeId, kubeletEndpoint)
	err := d.Run()
	klog.V(0).Infof("CSI driver run failed: %v", err)

}
