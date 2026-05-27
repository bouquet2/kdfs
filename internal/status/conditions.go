package status

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

func SetTrue(conditions []metav1.Condition, conditionType, reason, message string) []metav1.Condition {
	return set(conditions, conditionType, metav1.ConditionTrue, reason, message)
}

func SetFalse(conditions []metav1.Condition, conditionType, reason, message string) []metav1.Condition {
	return set(conditions, conditionType, metav1.ConditionFalse, reason, message)
}

func set(conditions []metav1.Condition, conditionType string, conditionStatus metav1.ConditionStatus, reason, message string) []metav1.Condition {
	now := metav1.Now()
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             conditionStatus,
		ObservedGeneration: 0,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}

	for i := range conditions {
		if conditions[i].Type == conditionType {
			conditions[i] = condition
			return conditions
		}
	}

	return append(conditions, condition)
}
