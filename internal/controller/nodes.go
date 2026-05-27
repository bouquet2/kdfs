package controller

import (
	"context"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	"github.com/bouquet2/kdfs/internal/names"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *EngineReconciler) getReplicaPod(ctx context.Context, replica *storagev1alpha1.Replica) *corev1.Pod {
	pods := &corev1.PodList{}
	r.List(ctx, pods, client.InNamespace(replica.Namespace), client.MatchingLabels{"kdfs.krea.to/mode": "replica"})
	for _, pod := range pods.Items {
		if pod.Name == replica.Name+"-pod" && pod.Status.PodIP != "" {
			return &pod
		}
	}
	return nil
}

func (r *EngineReconciler) getEnginePodInfo(ctx context.Context, engine *storagev1alpha1.Engine) (string, bool, bool) {
	if engine.Status.PodRef == nil {
		return "", false, false
	}
	pod := &corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{Name: engine.Status.PodRef.Name, Namespace: engine.Status.PodRef.Namespace}, pod); err != nil {
		return "", false, false
	}
	ready := pod.Status.PodIP != ""
	for _, status := range pod.Status.ContainerStatuses {
		if !status.Ready {
			ready = false
			break
		}
	}
	if len(pod.Status.ContainerStatuses) == 0 {
		ready = false
	}
	return pod.Status.PodIP, true, ready
}

func (r *EngineReconciler) allowedHosts(ctx context.Context, engineNode string) []string {
	var nodes corev1.NodeList
	if err := r.List(ctx, &nodes); err != nil {
		engineLogger.Warn().Err(err).Str("engine_node", engineNode).Msg("failed to list nodes for allowed hosts")
		return nil
	}
	var hosts []string
	for _, node := range nodes.Items {
		if _, isCP := node.Labels["node-role.kubernetes.io/control-plane"]; isCP {
			continue
		}
		hostNQN := names.HostNQN(node.Name)
		hosts = append(hosts, hostNQN)
	}
	return hosts
}

func (r *ReplicaReconciler) replicaPod(ctx context.Context, replica *storagev1alpha1.Replica) (*corev1.Pod, error) {
	pod := &corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{Name: replica.Name + "-pod", Namespace: replica.Namespace}, pod); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return pod, nil
}
