package dashboard

import (
	"context"
	"fmt"
	"html"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bouquet2/kdfs/internal/names"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Handler struct {
	Client    client.Client
	Namespace string
}

// Renders the volume list view and aggregates engine/PVC/node stats for the dashboard.
func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	var volumes storagev1alpha1.VolumeList
	if err := h.Client.List(ctx, &volumes, client.InNamespace(h.Namespace)); err != nil {
		http.Error(w, "failed to list volumes: "+err.Error(), http.StatusInternalServerError)
		return
	}
	var engines storagev1alpha1.EngineList
	if err := h.Client.List(ctx, &engines, client.InNamespace(h.Namespace)); err != nil {
		http.Error(w, "failed to list engines: "+err.Error(), http.StatusInternalServerError)
		return
	}
	engineCounts := make(map[string]int)
	for _, e := range engines.Items {
		if e.Spec.VolumeRef.Name != "" {
			engineCounts[e.Spec.VolumeRef.Name] = len(e.Spec.Replicas)
		}
	}
	var nodes corev1.NodeList
	if err := h.Client.List(ctx, &nodes); err != nil {
		http.Error(w, "failed to list nodes: "+err.Error(), http.StatusInternalServerError)
		return
	}
	var pvcs corev1.PersistentVolumeClaimList
	if err := h.Client.List(ctx, &pvcs, client.InNamespace(h.Namespace)); err != nil {
		http.Error(w, "failed to list PVCs: "+err.Error(), http.StatusInternalServerError)
		return
	}

	stats := computeStats(volumes.Items, engines.Items, pvcs.Items, nodes.Items)

	rows := make([]volumeRow, 0, len(volumes.Items))
	for _, v := range volumes.Items {
		count := h.effectiveReplicaCount(r.Context(), &v)
		if c, ok := engineCounts[v.Name]; ok && c > 0 {
			count = c
		}
		rows = append(rows, volumeRow{Volume: v, DisplayReplicaCount: count})
	}
	data := map[string]any{
		"Volumes": rows,
		"Stats":   stats,
	}
	w.Header().Set("Content-Type", "text/html")
	if r.Header.Get("HX-Request") == "true" {
		if err := Render(w, "list-body", data); err != nil {
			http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}
	if err := Render(w, "list", data); err != nil {
		http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) HandleCreateForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	if err := Render(w, "create", nil); err != nil {
		http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
	}
}

const maxKubernetesNameLength = 253

var namePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// Validates creation input, builds the volume CR, and returns HTMX-friendly responses.
func (h *Handler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form: "+err.Error(), http.StatusBadRequest)
		return
	}
	name := r.FormValue("name")
	size := r.FormValue("size")
	replicaCountStr := r.FormValue("replicaCount")

	w.Header().Set("Content-Type", "text/html")
	if name == "" || len(name) > maxKubernetesNameLength {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<mark style="color:red">Name is required (max 253 characters)</mark>`))
		return
	}
	if !namePattern.MatchString(name) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<mark style="color:red">Name must consist of lowercase letters, digits, and hyphens</mark>`))
		return
	}
	if !derivedVolumeNamesFit(name) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<mark style="color:red">Name is too long for generated engine, replica, and pod names</mark>`))
		return
	}
	sizeNum, err := strconv.Atoi(size)
	if err != nil || sizeNum <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<mark style="color:red">Size must be a positive integer (Gi)</mark>`))
		return
	}

	replicaCount := replicaCountStr
	if replicaCount == "" {
		replicaCount = "auto"
	}
	if _, _, err := storagev1alpha1.ParseReplicaCount(replicaCount); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<mark style="color:red">Replica count must be "auto" or a positive integer</mark>`))
		return
	}

	volume := &storagev1alpha1.Volume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: h.Namespace,
		},
		Spec: storagev1alpha1.VolumeSpec{
			Size:         size + "Gi",
			NodeID:       "",
			ReplicaCount: replicaCount,
		},
	}
	if err := h.Client.Create(r.Context(), volume); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`<mark style="color:red">Create failed: ` + html.EscapeString(err.Error()) + `</mark>`))
		return
	}
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) HandleDetail(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var volume storagev1alpha1.Volume
	if err := h.Client.Get(r.Context(), client.ObjectKey{Namespace: h.Namespace, Name: name}, &volume); err != nil {
		http.Error(w, "volume not found: "+err.Error(), http.StatusNotFound)
		return
	}
	replicaCount := h.effectiveReplicaCount(r.Context(), &volume)
	w.Header().Set("Content-Type", "text/html")
	if err := Render(w, "detail", map[string]any{"Volume": volume, "ReplicaCount": replicaCount}); err != nil {
		http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) HandleOverview(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var volume storagev1alpha1.Volume
	if err := h.Client.Get(r.Context(), client.ObjectKey{Namespace: h.Namespace, Name: name}, &volume); err != nil {
		http.Error(w, "volume not found: "+err.Error(), http.StatusNotFound)
		return
	}
	replicaCount := h.effectiveReplicaCount(r.Context(), &volume)
	w.Header().Set("Content-Type", "text/html")
	if err := Render(w, "overview", map[string]any{"Volume": volume, "ReplicaCount": replicaCount}); err != nil {
		http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) HandleReplicas(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var volume storagev1alpha1.Volume
	if err := h.Client.Get(r.Context(), client.ObjectKey{Namespace: h.Namespace, Name: name}, &volume); err != nil {
		h.renderEmptyReplicas(w)
		return
	}
	engine, err := h.getVolumeEngine(r.Context(), &volume)
	if err != nil || engine == nil {
		h.renderEmptyReplicas(w)
		return
	}
	views := make([]replicaView, 0, len(engine.Spec.Replicas))
	for _, rep := range engine.Spec.Replicas {
		var repCR storagev1alpha1.Replica
		phase := "Unknown"
		nqn := rep.NQN
		if err := h.Client.Get(r.Context(), client.ObjectKey{Namespace: h.Namespace, Name: rep.Name}, &repCR); err == nil {
			phase = string(repCR.Status.Phase)
			if repCR.Status.NQN != "" {
				nqn = repCR.Status.NQN
			}
		}
		views = append(views, replicaView{
			Name:    rep.Name,
			NodeID:  rep.NodeID,
			IsLocal: rep.NodeID == engine.Spec.NodeID,
			Phase:   phase,
			NQN:     nqn,
		})
	}
	w.Header().Set("Content-Type", "text/html")
	if err := Render(w, "replicas", map[string]any{"Replicas": views, "VolumeName": name}); err != nil {
		http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) HandleScaleForm(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var volume storagev1alpha1.Volume
	if err := h.Client.Get(r.Context(), client.ObjectKey{Namespace: h.Namespace, Name: name}, &volume); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	data := map[string]any{
		"Name":         name,
		"ReplicaCount": h.effectiveReplicaCount(r.Context(), &volume),
	}
	w.Header().Set("Content-Type", "text/html")
	if err := Render(w, "scale", data); err != nil {
		http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) HandleScale(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := r.ParseForm(); err != nil {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<mark style="color:red">Invalid form</mark>`))
		return
	}
	countStr := r.FormValue("replicaCount")
	count, auto, err := storagev1alpha1.ParseReplicaCount(countStr)
	if err != nil || auto {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<mark style="color:red">Replica count must be a positive integer</mark>`))
		return
	}
	patchBytes := fmt.Appendf(nil, `{"spec":{"replicaCount":%q}}`, strconv.Itoa(count))
	volume := &storagev1alpha1.Volume{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: h.Namespace},
	}
	if err := h.Client.Patch(r.Context(), volume, client.RawPatch(types.MergePatchType, patchBytes)); err != nil {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`<mark style="color:red">Scale failed: ` + html.EscapeString(err.Error()) + `</mark>`))
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<mark style="color:green">Scaled to ` + html.EscapeString(countStr) + ` replicas</mark>`))
}

// Deletes a replica CR, updates the engine attachments, and bumps the volume for reconciliation.
func (h *Handler) HandleDeleteReplica(w http.ResponseWriter, r *http.Request) {
	volumeName := r.PathValue("name")
	replicaName := r.PathValue("replica")

	replica := &storagev1alpha1.Replica{
		ObjectMeta: metav1.ObjectMeta{Name: replicaName, Namespace: h.Namespace},
	}
	if err := h.Client.Delete(r.Context(), replica); err != nil {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`<tr><td colspan="6"><mark style="color:red">Delete failed: ` + html.EscapeString(err.Error()) + `</mark></td></tr>`))
		return
	}

	var volume storagev1alpha1.Volume
	if err := h.Client.Get(r.Context(), client.ObjectKey{Namespace: h.Namespace, Name: volumeName}, &volume); err == nil {
		if engine, _ := h.getVolumeEngine(r.Context(), &volume); engine != nil {
			filtered := engine.Spec.Replicas[:0]
			for _, rep := range engine.Spec.Replicas {
				if rep.Name != replicaName {
					filtered = append(filtered, rep)
				}
			}
			if len(filtered) < len(engine.Spec.Replicas) {
				engine.Spec.Replicas = filtered
				if err := h.Client.Update(r.Context(), engine); err != nil {
					w.Header().Set("Content-Type", "text/html")
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(`<tr><td colspan="6"><mark style="color:red">Engine update failed: ` + html.EscapeString(err.Error()) + `</mark></td></tr>`))
					return
				}
			}
		}
		// Trigger volume reconciliation to ensure scale
		patchBytes := fmt.Appendf(nil, `{"metadata":{"annotations":{"kdfs.krea.to/last-heal":"%d"}}}`, time.Now().Unix())
		if err := h.Client.Patch(r.Context(), &volume, client.RawPatch(types.MergePatchType, patchBytes)); err != nil {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`<tr><td colspan="6"><mark style="color:red">Patch failed: ` + html.EscapeString(err.Error()) + `</mark></td></tr>`))
			return
		}
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`<tr><td colspan="6">Deleted</td></tr>`))
}

func (h *Handler) HandleDeleteForm(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	w.Header().Set("Content-Type", "text/html")
	if err := Render(w, "delete", map[string]string{"Name": name}); err != nil {
		http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	volume := &storagev1alpha1.Volume{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: h.Namespace},
	}
	if err := h.Client.Delete(r.Context(), volume); err != nil {
		http.Error(w, "delete failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) getVolumeEngine(ctx context.Context, volume *storagev1alpha1.Volume) (*storagev1alpha1.Engine, error) {
	if volume.Status.EngineRef == nil || volume.Status.EngineRef.Name == "" {
		return nil, nil
	}
	var engine storagev1alpha1.Engine
	if err := h.Client.Get(ctx, client.ObjectKey{Namespace: h.Namespace, Name: volume.Status.EngineRef.Name}, &engine); err != nil {
		return nil, err
	}
	return &engine, nil
}

func (h *Handler) effectiveReplicaCount(ctx context.Context, volume *storagev1alpha1.Volume) int {
	if engine, _ := h.getVolumeEngine(ctx, volume); engine != nil {
		return len(engine.Spec.Replicas)
	}
	count, auto, err := storagev1alpha1.ParseReplicaCount(volume.Spec.ReplicaCount)
	if err != nil || auto {
		return 0
	}
	return count
}

func (h *Handler) HandleSnapshots(w http.ResponseWriter, r *http.Request) {
	volumeName := r.PathValue("name")
	var snapshots storagev1alpha1.SnapshotList
	if err := h.Client.List(r.Context(), &snapshots, client.InNamespace(h.Namespace)); err != nil {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`<mark style="color:red">failed to list snapshots: ` + html.EscapeString(err.Error()) + `</mark>`))
		return
	}
	var filtered []storagev1alpha1.Snapshot
	for _, s := range snapshots.Items {
		if s.Spec.VolumeRef == volumeName {
			filtered = append(filtered, s)
		}
	}
	w.Header().Set("Content-Type", "text/html")
	if err := Render(w, "snapshots", map[string]any{"Snapshots": filtered, "VolumeName": volumeName}); err != nil {
		http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) HandleSnapshotDelete(w http.ResponseWriter, r *http.Request) {
	snapshotName := r.PathValue("snapshot")
	snapshot := &storagev1alpha1.Snapshot{
		ObjectMeta: metav1.ObjectMeta{Name: snapshotName, Namespace: h.Namespace},
	}
	if err := h.Client.Delete(r.Context(), snapshot); err != nil {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`<mark style="color:red">Delete failed: ` + html.EscapeString(err.Error()) + `</mark>`))
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<tr><td colspan="5">Deleted</td></tr>`))
}

func (h *Handler) renderEmptyReplicas(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html")
	if err := Render(w, "replicas", map[string]any{"Replicas": []replicaView{}}); err != nil {
		http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
	}
}

func derivedVolumeNamesFit(volumeName string) bool {
	engineCR := names.EngineName(volumeName)
	if len(engineCR) > maxKubernetesNameLength || len(engineCR+"-pod") > maxKubernetesNameLength {
		return false
	}
	for _, idx := range []int{0, 9, 99, 999} {
		replicaCR := names.ReplicaName(volumeName, idx)
		if len(replicaCR) > maxKubernetesNameLength || len(replicaCR+"-pod") > maxKubernetesNameLength {
			return false
		}
	}
	return true
}

type replicaView struct {
	Name    string
	NodeID  string
	IsLocal bool
	Phase   string
	NQN     string
}

type volumeRow struct {
	storagev1alpha1.Volume
	DisplayReplicaCount int
}

type statsData struct {
	TotalVolumes    int
	ReadyVolumes    int
	TotalSizeGi     string
	TotalPVCCount   int
	TotalPVCUsageGi string
	NodeCount       int
	Nodes           []nodeStat
}

type nodeStat struct {
	Name         string
	ReplicaCount int
	SizeGi       string
}

// Aggregates volume, replica, PVC, and node data into the dashboard stats view model.
func computeStats(volumes []storagev1alpha1.Volume, engines []storagev1alpha1.Engine, pvcs []corev1.PersistentVolumeClaim, nodes []corev1.Node) statsData {
	s := statsData{NodeCount: len(nodes)}
	var sizeTotal float64
	for _, v := range volumes {
		s.TotalVolumes++
		if v.Status.Phase == "Ready" {
			s.ReadyVolumes++
		}
		gi := parseSizeToGi(v.Spec.Size)
		sizeTotal += gi
	}
	s.TotalSizeGi = fmt.Sprintf("%.1f Gi", sizeTotal)

	volumeSizes := make(map[string]float64)
	for _, v := range volumes {
		volumeSizes[v.Name] = parseSizeToGi(v.Spec.Size)
	}
	nodeReplicas := make(map[string]int)
	nodeReplicaSize := make(map[string]float64)
	for _, e := range engines {
		size := volumeSizes[e.Spec.VolumeRef.Name]
		for _, rep := range e.Spec.Replicas {
			nodeReplicas[rep.NodeID]++
			nodeReplicaSize[rep.NodeID] += size
		}
	}
	for name, count := range nodeReplicas {
		s.Nodes = append(s.Nodes, nodeStat{Name: name, ReplicaCount: count, SizeGi: fmt.Sprintf("%.1f Gi", nodeReplicaSize[name])})
	}
	var pvcUsage float64
	for _, p := range pvcs {
		if p.Spec.StorageClassName != nil && *p.Spec.StorageClassName == "kdfs" {
			s.TotalPVCCount++
			if qty, ok := p.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
				pvcUsage += float64(qty.Value()) / (1024 * 1024 * 1024)
			}
		}
	}
	s.TotalPVCUsageGi = fmt.Sprintf("%.1f Gi", pvcUsage)
	return s
}

func parseSizeToGi(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if strings.HasSuffix(s, "Gi") {
		val, _ := strconv.ParseFloat(strings.TrimSuffix(s, "Gi"), 64)
		return val
	}
	if strings.HasSuffix(s, "Mi") {
		val, _ := strconv.ParseFloat(strings.TrimSuffix(s, "Mi"), 64)
		return val / 1024
	}
	if strings.HasSuffix(s, "Ki") {
		val, _ := strconv.ParseFloat(strings.TrimSuffix(s, "Ki"), 64)
		return val / (1024 * 1024)
	}
	val, _ := strconv.ParseFloat(s, 64)
	return math.Round(val/(1024*1024*1024)*10) / 10
}
