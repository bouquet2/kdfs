package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type ReplicaType string
type ReplicaPhase string

const (
	ReplicaTypeLocal  ReplicaType = "Local"
	ReplicaTypeRemote ReplicaType = "Remote"

	ReplicaPhasePending  ReplicaPhase = "Pending"
	ReplicaPhaseCreating ReplicaPhase = "Creating"
	ReplicaPhaseRunning  ReplicaPhase = "Running"
	ReplicaPhaseFailed   ReplicaPhase = "Failed"

	ReplicaConditionFilesystemCreated = "FilesystemCreated"
	ReplicaConditionBdevAttached      = "BdevAttached"
	ReplicaConditionNVMFExported      = "NVMFExported"
)

// +kubebuilder:object:generate=true
type ReplicaSpec struct {
	VolumeRef      LocalObjectReference `json:"volumeRef"`
	NodeID         string               `json:"nodeID"`
	Type           ReplicaType          `json:"type"`
	Size           string               `json:"size"`
	DataPath       string               `json:"dataPath"`
	SnapshotSource string               `json:"snapshotSource,omitempty"`
}

// +kubebuilder:object:generate=true
type ReplicaStatus struct {
	Phase      ReplicaPhase       `json:"phase,omitempty"`
	BdevName   string             `json:"bdevName,omitempty"`
	NQN        string             `json:"nqn,omitempty"`
	Endpoint   string             `json:"endpoint,omitempty"`
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=krep
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=`.spec.nodeID`
type Replica struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ReplicaSpec   `json:"spec,omitempty"`
	Status ReplicaStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ReplicaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Replica `json:"items"`
}
