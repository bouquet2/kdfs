package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	"github.com/bouquet2/kdfs/internal/agent"
	"github.com/bouquet2/kdfs/internal/controller"
	"github.com/bouquet2/kdfs/internal/logging"
	"github.com/bouquet2/kdfs/internal/network"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

const (
	kdfsNamespace       = "kdfs"
	kdfsConfigMapName   = "kdfs-config"
	nqnAuthorityKey     = "nqnAuthority"
	networkPolicyKey    = "networkPolicy"
	defaultNQNAuthority = "nqn.2026-05.krea.to"
	spdkImageEnvKey     = "KDFS_SPDK_IMAGE"
	sidecarImageEnvKey  = "KDFS_SIDECAR_IMAGE"
	nfsImageEnvKey      = "KDFS_NFS_SIDECAR_IMAGE"
)

func defaultNetworkPolicyJSON() string {
	b, err := json.Marshal(network.RolePolicies{
		Default: network.Policy{
			BindAddress:      "podIP",
			AdvertiseAddress: "podIP",
			PreferredFamily:  network.FamilyAuto,
		},
		Engine: network.Policy{
			Port: "4420",
		},
		Replica: network.Policy{
			Port: "4421",
		},
	})
	if err != nil {
		panic("default network policy: " + err.Error())
	}
	return string(b)
}

var serviceAccountNamespacePath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

var scheme = runtime.NewScheme()
var logger = logging.Component("controller-manager")

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(storagev1alpha1.AddToScheme(scheme))
}

func must(err error) {
	if err != nil {
		logger.Fatal().Err(err).Msg("controller-manager startup failed")
	}
}

func ensureNQNConfig(ctx context.Context, cl client.Client, namespace string) error {
	cm := &corev1.ConfigMap{}
	key := client.ObjectKey{Namespace: namespace, Name: kdfsConfigMapName}
	if err := cl.Get(ctx, key, cm); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		return cl.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: kdfsConfigMapName, Namespace: namespace},
			Data: map[string]string{
				nqnAuthorityKey:  defaultNQNAuthority,
				networkPolicyKey: defaultNetworkPolicyJSON(),
			},
		})
	}
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	updated := false
	if strings.TrimSpace(cm.Data[nqnAuthorityKey]) == "" {
		cm.Data[nqnAuthorityKey] = defaultNQNAuthority
		updated = true
	}
	if strings.TrimSpace(cm.Data[networkPolicyKey]) == "" {
		cm.Data[networkPolicyKey] = defaultNetworkPolicyJSON()
		updated = true
	}
	if !updated {
		return nil
	}
	return cl.Update(ctx, cm)
}

func nodeAgentBaseURL(ctx context.Context, cl client.Client, namespace, nodeName string) (string, error) {
	var pods corev1.PodList
	if err := cl.List(ctx, &pods, client.InNamespace(namespace)); err != nil {
		return "", err
	}
	for _, pod := range pods.Items {
		if pod.Labels["app"] != "kdfs-node-agent" {
			continue
		}
		if pod.Spec.NodeName != nodeName {
			continue
		}
		if pod.Status.Phase != corev1.PodRunning || strings.TrimSpace(pod.Status.PodIP) == "" {
			continue
		}
		return network.HTTPURL("http", pod.Status.PodIP, "9808", ""), nil
	}
	return "", fmt.Errorf("node-agent pod for node %s not found", nodeName)
}

func runtimeNamespace() string {
	if namespace := strings.TrimSpace(os.Getenv("POD_NAMESPACE")); namespace != "" {
		return namespace
	}
	if data, err := os.ReadFile(serviceAccountNamespacePath); err == nil {
		if namespace := strings.TrimSpace(string(data)); namespace != "" {
			return namespace
		}
	}
	return kdfsNamespace
}

func configureControllerPodImages() {
	if image := strings.TrimSpace(os.Getenv(spdkImageEnvKey)); image != "" {
		controller.PodImages.SPDK = image
	}
	if image := strings.TrimSpace(os.Getenv(sidecarImageEnvKey)); image != "" {
		controller.PodImages.Sidecar = image
	}
	if image := strings.TrimSpace(os.Getenv(nfsImageEnvKey)); image != "" {
		controller.PodImages.NFSSidecar = image
	}
}

func main() {
	var metricsAddr string
	var probeAddr string
	var leaderElect bool
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "metrics address")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "health probe address")
	flag.BoolVar(&leaderElect, "leader-elect", true, "enable leader election")
	flag.Parse()

	ctrl.SetLogger(logging.ControllerRuntime("controller-manager"))
	configureControllerPodImages()
	cfg := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme, Metrics: metricsserver.Options{BindAddress: metricsAddr}, HealthProbeBindAddress: probeAddr, LeaderElection: leaderElect, LeaderElectionID: "kdfs.storage.krea.to"})
	must(err)
	namespace := runtimeNamespace()
	bootstrapClient, err := client.New(cfg, client.Options{Scheme: scheme})
	must(err)
	must(ensureNQNConfig(context.Background(), bootstrapClient, namespace))
	replicaAgentFactory := func(ctx context.Context, nodeName string) (agent.Client, error) {
		baseURL, err := nodeAgentBaseURL(ctx, mgr.GetClient(), namespace, nodeName)
		if err != nil {
			return nil, err
		}
		return agent.NewHTTPClient(baseURL, 10*time.Second), nil
	}

	must((&controller.VolumeReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr))
	must((&controller.ReplicaReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), AgentFactory: replicaAgentFactory}).SetupWithManager(mgr))
	must((&controller.EngineReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr))
	must(mgr.AddHealthzCheck("healthz", healthz.Ping))
	must(mgr.AddReadyzCheck("readyz", healthz.Ping))
	must(mgr.Start(ctrl.SetupSignalHandler()))
}
