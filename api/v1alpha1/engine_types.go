package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type EnginePhase string

const (
	EnginePhasePending  EnginePhase = "Pending"
	EnginePhaseRunning  EnginePhase = "Running"
	EnginePhaseDegraded EnginePhase = "Degraded"
	EnginePhaseFailed   EnginePhase = "Failed"

	EngineConditionPodScheduled   = "PodScheduled"
	EngineConditionSPDKStarted    = "SPDKStarted"
	EngineConditionRAIDConfigured = "RAIDConfigured"
	EngineConditionSubsystemReady = "SubsystemReady"
)

// +kubebuilder:object:generate=true
type ReplicaAttachment struct {
	Name    string `json:"name"`
	NodeID  string `json:"nodeID"`
	IsLocal bool   `json:"isLocal,omitempty"`
	NQN     string `json:"nqn,omitempty"`
	Address string `json:"address,omitempty"`
	Port    string `json:"port,omitempty"`
}

// +kubebuilder:object:generate=true
type EngineSpec struct {
	VolumeRef LocalObjectReference `json:"volumeRef"`
	NodeID    string               `json:"nodeID"`
	Replicas  []ReplicaAttachment  `json:"replicas"`
}

type PodReference struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// +kubebuilder:object:generate=true
type EngineStatus struct {
	Phase        EnginePhase        `json:"phase,omitempty"`
	PodRef       *PodReference       `json:"podRef,omitempty"`
	Endpoint        string             `json:"endpoint,omitempty"`
	SubsystemNQN    string             `json:"subsystemNQN,omitempty"`
	ROXSubsystemNQN string             `json:"roxSubsystemNQN,omitempty"`
	ROXEndpoint      string             `json:"roxEndpoint,omitempty"`
	LastReplicasHash string             `json:"lastReplicasHash,omitempty"`
	Conditions       []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=keng
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=`.spec.nodeID`
type Engine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EngineSpec   `json:"spec,omitempty"`
	Status EngineStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type EngineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Engine `json:"items"`
}
