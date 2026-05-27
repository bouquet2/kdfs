package controller

import (
	"encoding/json"
	"fmt"
	"strings"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	"github.com/bouquet2/kdfs/internal/names"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Images struct {
	SPDK       string
	Sidecar    string
	NFSSidecar string
}

var PodImages = Images{
	SPDK:       "ghcr.io/bouquet2/kdfs/kdfs-spdk:dev",
	Sidecar:    "ghcr.io/bouquet2/kdfs/kdfs-sidecar:dev",
	NFSSidecar: "ghcr.io/bouquet2/kdfs/kdfs-nfs-sidecar:dev",
}

func EnginePodFor(engine *storagev1alpha1.Engine, allowedHosts []string) *corev1.Pod {
	volumeName := engine.Spec.VolumeRef.Name
	replicasJSON, _ := json.Marshal(engine.Spec.Replicas)
	env := []corev1.EnvVar{
		{Name: "KDFS_VOLUME_NAME", Value: volumeName},
		{Name: "KDFS_LOCAL_PATH", Value: "/data/" + volumeName + "/vol.img"},
		{Name: "KDFS_REPLICAS", Value: string(replicasJSON)},
		nqnAuthorityEnv(),
	}
	env = append(env, runtimeEnv()...)
	allowedHosts = sanitizeAllowedHosts(allowedHosts)
	if len(allowedHosts) > 0 {
		env = append(env, corev1.EnvVar{Name: "KDFS_ALLOWED_HOSTS", Value: strings.Join(allowedHosts, ",")})
	}
	env = append(env, mypodIPEnv())
	spdkPrivileged := true
	nfsPrivileged := true
	pod := spdkPod(engine.Name+"-pod", engine.Namespace, engine.Spec.NodeID, "spdk-engine", "engine", env, &spdkPrivileged)
	pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{
		Name: "nfs", Image: PodImages.NFSSidecar,
		Env: []corev1.EnvVar{
			{Name: "KDFS_NFS_EXPORT", Value: "/data/" + volumeName},
			{Name: "KDFS_NFS_BIND", Value: ":2049"},
		},
		VolumeMounts:    []corev1.VolumeMount{{Name: "data", MountPath: "/data"}},
		SecurityContext: &corev1.SecurityContext{Privileged: &nfsPrivileged},
	})
	return pod
}

func ReplicaPodFor(replica *storagev1alpha1.Replica) *corev1.Pod {
	volumeName := replica.Spec.VolumeRef.Name
	var idx int
	fmt.Sscanf(replica.Name, volumeName+"-replica-%d", &idx)
	env := []corev1.EnvVar{
		{Name: "KDFS_VOLUME_NAME", Value: volumeName},
		{Name: "KDFS_DATA_PATH", Value: "/data/" + volumeName + "/vol.img"},
		{Name: "KDFS_REPLICA_INDEX", Value: fmt.Sprintf("%d", idx)},
		nqnAuthorityEnv(),
		mypodIPEnv(),
	}
	env = append(env, runtimeEnv()...)
	spdkPrivileged := true
	return spdkPod(replica.Name+"-pod", replica.Namespace, replica.Spec.NodeID, "spdk-replica", "replica", env, &spdkPrivileged)
}

func runtimeEnv() []corev1.EnvVar {
	optional := true
	return []corev1.EnvVar{
		{Name: "KDFS_POD_IP", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"}}},
		{Name: "KDFS_HOST_IP", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"}}},
		{Name: "KDFS_NODE_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"}}},
		{Name: "KDFS_NETWORK_POLICY", ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "kdfs-config"}, Key: "networkPolicy", Optional: &optional}}},
	}
}

func mypodIPEnv() corev1.EnvVar {
	return corev1.EnvVar{
		Name: "KDFS_LISTEN_ADDR",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
		},
	}
}

func nqnAuthorityEnv() corev1.EnvVar {
	return corev1.EnvVar{
		Name: "NQN_AUTHORITY",
		ValueFrom: &corev1.EnvVarSource{
			ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "kdfs-config"},
				Key:                  "nqnAuthority",
			},
		},
	}
}

func spdkPod(name, namespace, nodeName, spdkContainer, mode string, sidecarEnv []corev1.EnvVar, privileged *bool) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: map[string]string{"app.kubernetes.io/name": "kdfs", "kdfs.krea.to/mode": mode}},
		Spec: corev1.PodSpec{
			NodeName:      nodeName,
			RestartPolicy: corev1.RestartPolicyAlways,
			Volumes: []corev1.Volume{
				{Name: "data", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/var/lib/kdfs"}}},
				{Name: "spdk-socket", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			},
			Containers: []corev1.Container{
				{Name: spdkContainer, Image: PodImages.SPDK, Args: []string{"--no-huge", "-s", "1024", "-r", "/var/tmp/spdk.sock"}, SecurityContext: &corev1.SecurityContext{Privileged: privileged}, VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/data"}, {Name: "spdk-socket", MountPath: "/var/tmp"}}},
				{Name: "sidecar", Image: PodImages.Sidecar, Args: []string{"--mode", mode}, Env: sidecarEnv, VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/data"}, {Name: "spdk-socket", MountPath: "/var/tmp"}}},
			},
		},
	}
}

func sanitizeAllowedHosts(hosts []string) []string {
	filtered := hosts[:0]
	for _, host := range hosts {
		if names.IsHostNQN(host) {
			filtered = append(filtered, host)
		}
	}
	return filtered
}
