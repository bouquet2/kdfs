//go:build tools

package tools

import (
	_ "github.com/container-storage-interface/spec/lib/go/csi"
	_ "github.com/go-logr/logr"
	_ "github.com/onsi/ginkgo/v2"
	_ "github.com/onsi/gomega"
	_ "google.golang.org/grpc"
	_ "k8s.io/api/core/v1"
	_ "k8s.io/apimachinery/pkg/runtime"
	_ "k8s.io/client-go/kubernetes"
	_ "sigs.k8s.io/controller-runtime"
)
