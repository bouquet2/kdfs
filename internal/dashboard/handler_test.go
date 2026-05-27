package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func newCreateRequest(form url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/volumes/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestHandleCreateKeepsAutoReplicaCount(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(testScheme(t)).WithRuntimeObjects(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-1"}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-2"}},
	).Build()
	h := &Handler{Client: cl, Namespace: "kdfs"}
	w := httptest.NewRecorder()

	h.HandleCreate(w, newCreateRequest(url.Values{
		"name":         {"auto-volume"},
		"size":         {"10"},
		"replicaCount": {"auto"},
	}))

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	var volume storagev1alpha1.Volume
	if err := cl.Get(context.Background(), client.ObjectKey{Namespace: "kdfs", Name: "auto-volume"}, &volume); err != nil {
		t.Fatal(err)
	}
	if volume.Spec.ReplicaCount != "auto" {
		t.Fatalf("expected auto replica count to be stored as %q, got %q", "auto", volume.Spec.ReplicaCount)
	}
}

func TestHandleCreateKeepsExplicitReplicaCountString(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	h := &Handler{Client: cl, Namespace: "kdfs"}
	w := httptest.NewRecorder()

	h.HandleCreate(w, newCreateRequest(url.Values{
		"name":         {"explicit-volume"},
		"size":         {"10"},
		"replicaCount": {"3"},
	}))

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	var volume storagev1alpha1.Volume
	if err := cl.Get(context.Background(), client.ObjectKey{Namespace: "kdfs", Name: "explicit-volume"}, &volume); err != nil {
		t.Fatal(err)
	}
	if volume.Spec.ReplicaCount != "3" {
		t.Fatalf("expected explicit replica count to be stored as %q, got %q", "3", volume.Spec.ReplicaCount)
	}
}

func TestHandleCreateRejectsPartialReplicaCount(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	h := &Handler{Client: cl, Namespace: "kdfs"}
	w := httptest.NewRecorder()

	h.HandleCreate(w, newCreateRequest(url.Values{
		"name":         {"bad-replica-count"},
		"size":         {"10"},
		"replicaCount": {"1abc"},
	}))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestHandleCreateRejectsNamesThatBreakChildNames(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	h := &Handler{Client: cl, Namespace: "kdfs"}
	w := httptest.NewRecorder()

	h.HandleCreate(w, newCreateRequest(url.Values{
		"name":         {strings.Repeat("a", 240)},
		"size":         {"10"},
		"replicaCount": {"1"},
	}))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestHandleScaleRejectsNegativeReplicaCount(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(testScheme(t)).WithRuntimeObjects(
		&storagev1alpha1.Volume{ObjectMeta: metav1.ObjectMeta{Name: "test-vol", Namespace: "kdfs"}},
	).Build()
	h := &Handler{Client: cl, Namespace: "kdfs"}
	w := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodPost, "/volumes/test-vol/scale", strings.NewReader(url.Values{
		"replicaCount": {"-1"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	h.HandleScale(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestHandleCreateRejectsZeroReplicaCount(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	h := &Handler{Client: cl, Namespace: "kdfs"}
	w := httptest.NewRecorder()

	h.HandleCreate(w, newCreateRequest(url.Values{
		"name":         {"zero-replica-count"},
		"size":         {"10"},
		"replicaCount": {"0"},
	}))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestHandleScaleRejectsZeroReplicaCount(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(testScheme(t)).WithRuntimeObjects(
		&storagev1alpha1.Volume{ObjectMeta: metav1.ObjectMeta{Name: "test-vol", Namespace: "kdfs"}},
	).Build()
	h := &Handler{Client: cl, Namespace: "kdfs"}
	w := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodPost, "/volumes/test-vol/scale", strings.NewReader(url.Values{
		"replicaCount": {"0"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	h.HandleScale(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestHandleScaleFormUsesEffectiveReplicaCountForAutoVolume(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(testScheme(t)).WithRuntimeObjects(
		&storagev1alpha1.Volume{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vol", Namespace: "kdfs"},
			Spec:       storagev1alpha1.VolumeSpec{ReplicaCount: "auto"},
			Status: storagev1alpha1.VolumeStatus{
				EngineRef: &storagev1alpha1.NamespacedObjectReference{Name: "test-vol-engine", Namespace: "kdfs"},
			},
		},
		&storagev1alpha1.Engine{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vol-engine", Namespace: "kdfs"},
			Spec: storagev1alpha1.EngineSpec{
				VolumeRef: storagev1alpha1.LocalObjectReference{Name: "test-vol"},
				Replicas: []storagev1alpha1.ReplicaAttachment{
					{Name: "replica-0", NodeID: "worker-1"},
					{Name: "replica-1", NodeID: "worker-2"},
				},
			},
		},
	).Build()
	h := &Handler{Client: cl, Namespace: "kdfs"}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/volumes/test-vol/scale", nil)
	req.SetPathValue("name", "test-vol")

	h.HandleScaleForm(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `value="2"`) {
		t.Fatalf("expected scale form to render effective replica count, got %s", w.Body.String())
	}
}

func TestDerivedVolumeNamesFit(t *testing.T) {
	if !derivedVolumeNamesFit("short") {
		t.Error("expected short name to fit")
	}
	if derivedVolumeNamesFit(strings.Repeat("a", 240)) {
		t.Error("expected 240-char name to fail due to replica pod name")
	}
	if !derivedVolumeNamesFit(strings.Repeat("a", 230)) {
		t.Error("expected 230-char name to fit")
	}
}

func TestHandleDeleteReplicaMissingWriteHeader(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(testScheme(t)).WithRuntimeObjects(
		&storagev1alpha1.Volume{ObjectMeta: metav1.ObjectMeta{Name: "test-vol", Namespace: "kdfs"}},
		&storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: "replica-0", Namespace: "kdfs"}},
		&storagev1alpha1.Engine{ObjectMeta: metav1.ObjectMeta{Name: "test-vol-engine", Namespace: "kdfs"}, Spec: storagev1alpha1.EngineSpec{Replicas: []storagev1alpha1.ReplicaAttachment{{Name: "replica-0", NodeID: "node-1"}}}},
	).Build()
	h := &Handler{Client: cl, Namespace: "kdfs"}
	w := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodPost, "/volumes/test-vol/replicas/replica-0/delete", strings.NewReader(url.Values{}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	h.HandleDeleteReplica(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleScaleAcceptsEmptyString(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(testScheme(t)).WithRuntimeObjects(
		&storagev1alpha1.Volume{ObjectMeta: metav1.ObjectMeta{Name: "test-vol", Namespace: "kdfs"}},
	).Build()
	h := &Handler{Client: cl, Namespace: "kdfs"}
	w := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodPost, "/volumes/test-vol/scale", strings.NewReader(url.Values{
		"replicaCount": {""},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	h.HandleScale(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestHandleScaleAcceptsInvalidString(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(testScheme(t)).WithRuntimeObjects(
		&storagev1alpha1.Volume{ObjectMeta: metav1.ObjectMeta{Name: "test-vol", Namespace: "kdfs"}},
	).Build()
	h := &Handler{Client: cl, Namespace: "kdfs"}
	w := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodPost, "/volumes/test-vol/scale", strings.NewReader(url.Values{
		"replicaCount": {"abc"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	h.HandleScale(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}
