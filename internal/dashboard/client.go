package dashboard

import (
	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var Scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(Scheme))
	utilruntime.Must(storagev1alpha1.AddToScheme(Scheme))
}

func NewClient(cfg *rest.Config) (client.Client, error) {
	return client.New(cfg, client.Options{Scheme: Scheme})
}
