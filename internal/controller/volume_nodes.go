package controller

import (
	"context"
	"fmt"
	"sort"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

func (r *VolumeReconciler) pickUnusedNode(ctx context.Context, used map[string]bool) string {
	nodes := &corev1.NodeList{}
	if err := r.List(ctx, nodes); err != nil {
		return ""
	}
	for _, node := range nodes.Items {
		if _, isCP := node.Labels["node-role.kubernetes.io/control-plane"]; isCP {
			continue
		}
		if !used[node.Name] {
			return node.Name
		}
	}
	for _, node := range nodes.Items {
		if _, isCP := node.Labels["node-role.kubernetes.io/control-plane"]; !isCP {
			return node.Name
		}
	}
	return ""
}

func (r *VolumeReconciler) pickAnyWorkerNode(ctx context.Context) (string, error) {
	nodes := &corev1.NodeList{}
	if err := r.List(ctx, nodes); err != nil {
		return "", err
	}
	for _, node := range nodes.Items {
		if _, isMaster := node.Labels["node-role.kubernetes.io/control-plane"]; isMaster {
			continue
		}
		return node.Name, nil
	}
	if len(nodes.Items) > 0 {
		return nodes.Items[0].Name, nil
	}
	return "", fmt.Errorf("no worker nodes available")
}

func (r *VolumeReconciler) replicasForVolume(ctx context.Context, volume *storagev1alpha1.Volume) (int, error) {
	count, auto, err := storagev1alpha1.ParseReplicaCount(volume.Spec.ReplicaCount)
	if err != nil {
		return 0, err
	}
	if !auto {
		return count, nil
	}
	nodes := &corev1.NodeList{}
	if err := r.List(ctx, nodes); err != nil {
		return 0, err
	}
	count = 0
	for _, node := range nodes.Items {
		if _, isCP := node.Labels["node-role.kubernetes.io/control-plane"]; !isCP {
			count++
		}
	}
	if count == 0 && len(nodes.Items) > 0 {
		count = len(nodes.Items)
	}
	if count < 1 {
		count = 1
	}
	return count, nil
}

func (r *VolumeReconciler) pickWorkerNodes(ctx context.Context, n int) ([]string, error) {
	nodes := &corev1.NodeList{}
	if err := r.List(ctx, nodes); err != nil {
		return nil, err
	}
	var workers []string
	for _, node := range nodes.Items {
		if _, isCP := node.Labels["node-role.kubernetes.io/control-plane"]; !isCP {
			workers = append(workers, node.Name)
		}
	}
	if len(workers) == 0 {
		for _, node := range nodes.Items {
			workers = append(workers, node.Name)
		}
	}
	sort.Strings(workers)
	if n > len(workers) {
		n = len(workers)
	}
	return workers[:n], nil
}
