package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const GroupName = "storage.krea.to"

var GroupVersion = schema.GroupVersion{Group: GroupName, Version: "v1alpha1"}

var SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)
var AddToScheme = SchemeBuilder.AddToScheme

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion, &Volume{}, &VolumeList{}, &Engine{}, &EngineList{}, &Replica{}, &ReplicaList{})
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
