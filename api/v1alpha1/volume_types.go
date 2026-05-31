package v1alpha1

import (
	"fmt"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type VolumePhase string

const (
	VolumePhaseCreating VolumePhase = "Creating"
	VolumePhaseReady    VolumePhase = "Ready"
	VolumePhaseDegraded VolumePhase = "Degraded"
	VolumePhaseFailed   VolumePhase = "Failed"

	VolumeConditionScheduled     = "Scheduled"
	VolumeConditionEngineReady   = "EngineReady"
	VolumeConditionReplicasReady = "ReplicasReady"
	VolumeConditionReplicasHealing = "ReplicasHealing"
)

// +kubebuilder:object:generate=true
type ReplicaHealth struct {
	Name            string      `json:"name"`
	NodeID          string      `json:"nodeID"`
	Phase           string      `json:"phase"`
	RestartAttempts int         `json:"restartAttempts,omitempty"`
	LastHealTime    *metav1.Time `json:"lastHealTime,omitempty"`
}

type LocalObjectReference struct {
	Name string `json:"name"`
}

type NamespacedObjectReference struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// +kubebuilder:object:generate=true
type SnapshotSource struct {
	SnapshotName string `json:"snapshotName"`
}

// +kubebuilder:object:generate=true
type VolumeSpec struct {
	Size             string           `json:"size"`
	StorageClassName string           `json:"storageClassName,omitempty"`
	NodeID           string           `json:"nodeID"`
	ReplicaCount     string           `json:"replicaCount,omitempty"`
	SnapshotSource   *SnapshotSource  `json:"snapshotSource,omitempty"`
}

func ParseReplicaCount(value string) (count int, auto bool, err error) {
	if value == "auto" || value == "" {
		return 0, true, nil
	}
	count, err = strconv.Atoi(value)
	if err != nil || count <= 0 {
		return 0, false, fmt.Errorf("invalid replicaCount %q: must be \"auto\" or a positive integer", value)
	}
	return count, false, nil
}

// +kubebuilder:object:generate=true
type VolumeStatus struct {
	Phase      VolumePhase                 `json:"phase,omitempty"`
	EngineRef      *NamespacedObjectReference `json:"engineRef,omitempty"`
	ReplicaHealth  []ReplicaHealth            `json:"replicaHealth,omitempty"`
	Conditions     []metav1.Condition         `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=kvol
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=`.spec.nodeID`
type Volume struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VolumeSpec   `json:"spec,omitempty"`
	Status VolumeStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type VolumeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Volume `json:"items"`
}
