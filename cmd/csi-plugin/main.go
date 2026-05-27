//go:build linux

package main

import (
	"flag"
	"net"
	"os"

	"github.com/bouquet2/kdfs/internal/csi"
	"github.com/bouquet2/kdfs/internal/logging"
	csipb "github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Boots the CSI driver, builds the Kubernetes client, and serves gRPC on the configured endpoint.
func main() {
	logger := logging.Component("csi-plugin")
	endpoint := flag.String("csi-address", "unix:///csi/csi.sock", "CSI gRPC endpoint")
	nodeID := flag.String("node-id", os.Getenv("KUBE_NODE_NAME"), "node ID")
	flag.Parse()

	proto, addr, err := parseEndpoint(*endpoint)
	if err != nil {
		logger.Fatal().Err(err).Str("endpoint", *endpoint).Msg("invalid CSI endpoint")
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		logger.Warn().Err(err).Msg("not in cluster, using empty client config")
		config = &rest.Config{}
	}

	scheme := runtime.NewScheme()
	_ = storagev1alpha1.AddToScheme(scheme)

	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to create k8s client")
	}

	driver := &csi.Driver{
		Client:    k8sClient,
		NodeID:    *nodeID,
		Namespace: "kdfs",
	}

	if proto == "unix" {
		os.Remove(addr)
	}

	listener, err := net.Listen(proto, addr)
	if err != nil {
		logger.Fatal().Err(err).Str("endpoint", *endpoint).Msg("failed to listen for CSI server")
	}

	server := grpc.NewServer()
	csipb.RegisterIdentityServer(server, driver)
	csipb.RegisterControllerServer(server, driver)
	csipb.RegisterNodeServer(server, driver)

	logger.Info().Str("endpoint", *endpoint).Str("node_id", *nodeID).Msg("CSI driver listening")
	if err := server.Serve(listener); err != nil {
		logger.Fatal().Err(err).Msg("CSI grpc server exited")
	}
}

// Splits a CSI endpoint into protocol and address, returning empty values for unsupported schemes.
func parseEndpoint(ep string) (string, string, error) {
	if len(ep) >= 6 && ep[:7] == "unix://" {
		return "unix", ep[7:], nil
	}
	if len(ep) >= 5 && ep[:5] == "tcp://" {
		return "tcp", ep[5:], nil
	}
	return "", "", nil
}
