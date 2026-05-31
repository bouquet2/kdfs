package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type SnapshotPhase string

const (
	SnapshotPhasePending  SnapshotPhase = "Pending"
	SnapshotPhaseCreating SnapshotPhase = "Creating"
	SnapshotPhaseReady    SnapshotPhase = "Ready"
	SnapshotPhaseFailed   SnapshotPhase = "Failed"

	SnapshotConditionCreated   = "SnapshotCreated"
	SnapshotConditionFileReady = "SnapshotFileReady"
)

// +kubebuilder:object:generate=true
type SnapshotSpec struct {
	VolumeRef  string `json:"volumeRef"`
	SnapshotID string `json:"snapshotID"`
}

// +kubebuilder:object:generate=true
type SnapshotStatus struct {
	Phase        SnapshotPhase      `json:"phase,omitempty"`
	SnapshotPath string             `json:"snapshotPath,omitempty"`
	EngineNode   string             `json:"engineNode,omitempty"`
	SizeBytes    int64              `json:"sizeBytes,omitempty"`
	CreationTime *metav1.Time       `json:"creationTime,omitempty"`
	ReadyToUse   bool               `json:"readyToUse"`
	Conditions   []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=kvolsnap
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Volume",type=string,JSONPath=`.spec.volumeRef`
type Snapshot struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              SnapshotSpec   `json:"spec,omitempty"`
	Status            SnapshotStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type SnapshotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Snapshot `json:"items"`
}
