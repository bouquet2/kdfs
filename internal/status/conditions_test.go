package status

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSetTrueConditionAddsCondition(t *testing.T) {
	conditions := SetTrue(nil, "Ready", "Configured", "resource is ready")
	if len(conditions) != 1 {
		t.Fatalf("len(conditions) = %d", len(conditions))
	}
	if conditions[0].Type != "Ready" || conditions[0].Status != metav1.ConditionTrue {
		t.Fatalf("condition = %#v", conditions[0])
	}
}

func TestSetFalseConditionReplacesCondition(t *testing.T) {
	conditions := SetTrue(nil, "Ready", "Configured", "resource is ready")
	conditions = SetFalse(conditions, "Ready", "Waiting", "resource is waiting")
	if len(conditions) != 1 {
		t.Fatalf("len(conditions) = %d", len(conditions))
	}
	if conditions[0].Status != metav1.ConditionFalse || conditions[0].Reason != "Waiting" {
		t.Fatalf("condition = %#v", conditions[0])
	}
}
