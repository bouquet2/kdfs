package controller

import (
	"context"
	"testing"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := storagev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func newTestVolume(name, namespace, node string) *storagev1alpha1.Volume {
	return &storagev1alpha1.Volume{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: storagev1alpha1.VolumeSpec{
			Size:             "10Gi",
			StorageClassName: "kdfs",
			NodeID:           node,
		},
	}
}

func newFakeClient(t *testing.T, objects ...runtime.Object) *fake.ClientBuilder {
	t.Helper()
	return fake.NewClientBuilder().WithScheme(testScheme(t)).WithRuntimeObjects(objects...)
}

var _ = context.Background
