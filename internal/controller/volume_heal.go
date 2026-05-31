package controller

import (
	"context"
	"fmt"
	"time"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
	statusutil "github.com/bouquet2/kdfs/internal/status"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func healingBackoff(attempts int) (backoff time.Duration, exhausted bool) {
	const maxAttempts = 5
	if attempts >= maxAttempts {
		return 0, true
	}
	base := 30 * time.Second
	for i := 0; i < attempts; i++ {
		base *= 2
	}
	return base, false
}

// healReplicas attempts to heal unhealthy replicas using a tiered strategy:
// 1. Missing replica CR → remove stale engine attachment (ensureScale picks up)
// 2. Pod exists but not healthy → restart pod (exponential backoff)
// 3. Exhausted retries → delete replica CR and remove attachment
// Only heals if healthyCount >= desired-1 (N-1 guard).
func (r *VolumeReconciler) healReplicas(ctx context.Context, volume *storagev1alpha1.Volume, engine *storagev1alpha1.Engine) (healed bool, requeueAfter time.Duration, err error) {
	log := ctrl.LoggerFrom(ctx)

	desired, err := r.replicasForVolume(ctx, volume)
	if err != nil {
		return false, 0, err
	}
	newHealth := make([]storagev1alpha1.ReplicaHealth, 0, len(engine.Spec.Replicas))
	healthyCount := 0

	for i := range engine.Spec.Replicas {
		ra := &engine.Spec.Replicas[i]

		health := storagev1alpha1.ReplicaHealth{
			Name:   ra.Name,
			NodeID: ra.NodeID,
			Phase:  "Unknown",
		}
		existing := findExistingHealth(volume.Status.ReplicaHealth, ra.Name)
		health.RestartAttempts = existing.RestartAttempts
		health.LastHealTime = existing.LastHealTime

		var replica storagev1alpha1.Replica
		if err := r.Get(ctx, client.ObjectKey{Namespace: volume.Namespace, Name: ra.Name}, &replica); err != nil {
			if apierrors.IsNotFound(err) {
				health.Phase = "Missing"
				newHealth = append(newHealth, health)
				continue
			}
			return false, 0, err
		}

		phase := string(replica.Status.Phase)
		health.Phase = phase

		if phase == "Running" {
			pod := r.getReplicaPodByName(ctx, volume.Namespace, ra.Name)
			podFound := pod != nil
			podPhase := ""
			if podFound {
				podPhase = string(pod.Status.Phase)
			}
			if podFound && pod.Status.Phase == corev1.PodRunning && replica.Status.NQN != "" {
				healthyCount++
				newHealth = append(newHealth, health)
				continue
			}
			log.Info("replica not healthy", "name", ra.Name, "phase", phase,
				"podFound", podFound, "podPhase", podPhase,
				"nqn", replica.Status.NQN)
		}

		health.Phase = phase
		if phase == "Running" {
			health.Phase = "Pending"
		}
		newHealth = append(newHealth, health)
	}

	volume.Status.ReplicaHealth = newHealth

	threshold := desired - 1
	if threshold < 0 {
		threshold = 0
	}
	if healthyCount < threshold {
		log.Info("not enough healthy replicas to safely heal", "healthy", healthyCount, "desired", desired)
		volume.Status.Phase = storagev1alpha1.VolumePhaseDegraded
		volume.Status.Conditions = statusutil.SetFalse(volume.Status.Conditions, storagev1alpha1.VolumeConditionReplicasHealing, "BelowThreshold",
			fmt.Sprintf("only %d healthy replicas, need at least %d", healthyCount, threshold))
		return false, 0, r.Status().Update(ctx, volume)
	}

	shortestCooldown := time.Duration(1<<63 - 1) // max duration

	for i := range engine.Spec.Replicas {
		ra := &engine.Spec.Replicas[i]
		health := &newHealth[i]

		switch health.Phase {
		case "Missing":
			log.Info("removing stale attachment for missing replica", "replica", ra.Name)
			engine.Spec.Replicas = removeAttachment(engine.Spec.Replicas, i)
			if err := r.Update(ctx, engine); err != nil {
				return false, 0, err
			}
			volume.Status.Conditions = statusutil.SetTrue(volume.Status.Conditions, storagev1alpha1.VolumeConditionReplicasHealing, "RemovedMissing",
				fmt.Sprintf("removed stale attachment for missing replica %s", ra.Name))
			return true, 0, r.Status().Update(ctx, volume)

		case "Failed", "Pending":
			pod := r.getReplicaPodByName(ctx, volume.Namespace, ra.Name)
			if pod == nil {
				var replicaCR storagev1alpha1.Replica
				if err := r.Get(ctx, client.ObjectKey{Namespace: volume.Namespace, Name: ra.Name}, &replicaCR); err != nil {
					continue
				}
				if replicaCR.Status.Phase != storagev1alpha1.ReplicaPhaseRunning {
					continue
				}
				if replicaCR.DeletionTimestamp != nil {
					continue
				}
				log.Info("replica pod missing, deleting replica for recreation", "replica", ra.Name)
				replica := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: ra.Name, Namespace: volume.Namespace}}
				if err := r.Delete(ctx, replica); err != nil && !apierrors.IsNotFound(err) {
					return false, 0, err
				}
				engine.Spec.Replicas = removeAttachment(engine.Spec.Replicas, i)
				if err := r.Update(ctx, engine); err != nil {
					return false, 0, err
				}
				volume.Status.Conditions = statusutil.SetTrue(volume.Status.Conditions, storagev1alpha1.VolumeConditionReplicasHealing, "Recreating",
					fmt.Sprintf("recreating replica %s (pod missing)", ra.Name))
				return true, 0, r.Status().Update(ctx, volume)
			}

			backoff, exhausted := healingBackoff(health.RestartAttempts)
			if exhausted {
				log.Info("exhausted restart attempts, deleting replica for recreation", "replica", ra.Name, "attempts", health.RestartAttempts)
				replica := &storagev1alpha1.Replica{ObjectMeta: metav1.ObjectMeta{Name: ra.Name, Namespace: volume.Namespace}}
				if err := r.Delete(ctx, replica); err != nil && !apierrors.IsNotFound(err) {
					return false, 0, err
				}
				engine.Spec.Replicas = removeAttachment(engine.Spec.Replicas, i)
				if err := r.Update(ctx, engine); err != nil {
					return false, 0, err
				}
				volume.Status.Conditions = statusutil.SetTrue(volume.Status.Conditions, storagev1alpha1.VolumeConditionReplicasHealing, "Recreating",
					fmt.Sprintf("deleting exhausted replica %s for recreation", ra.Name))
				return true, 0, r.Status().Update(ctx, volume)
			}

			if health.LastHealTime != nil {
				if remaining := backoff - time.Since(health.LastHealTime.Time); remaining > 0 {
					if remaining < shortestCooldown {
						shortestCooldown = remaining
					}
					continue
				}
			}

			log.Info("restarting replica pod", "replica", ra.Name, "attempt", health.RestartAttempts+1)
			if err := r.Delete(ctx, pod); err != nil && !apierrors.IsNotFound(err) {
				return false, 0, err
			}

			health.RestartAttempts++
			now := metav1.Now()
			health.LastHealTime = &now
			health.Phase = "Pending"

			volume.Status.Conditions = statusutil.SetTrue(volume.Status.Conditions, storagev1alpha1.VolumeConditionReplicasHealing, "RestartingPod",
				fmt.Sprintf("restarting pod for replica %s (attempt %d)", ra.Name, health.RestartAttempts))

			if backoff < shortestCooldown {
				shortestCooldown = backoff
			}
			return true, 0, r.Status().Update(ctx, volume)
		}

		if health.LastHealTime != nil {
			backoff, _ := healingBackoff(health.RestartAttempts)
			if remaining := backoff - time.Since(health.LastHealTime.Time); remaining > 0 {
				if remaining < shortestCooldown {
					shortestCooldown = remaining
				}
			}
		}
	}

	volume.Status.Conditions = statusutil.SetFalse(volume.Status.Conditions, storagev1alpha1.VolumeConditionReplicasHealing, "NoActionNeeded", "no replicas need healing")

	if shortestCooldown != time.Duration(1<<63-1) {
		return false, shortestCooldown, r.Status().Update(ctx, volume)
	}

	return false, 0, r.Status().Update(ctx, volume)
}

func (r *VolumeReconciler) getReplicaPodByName(ctx context.Context, namespace, replicaName string) *corev1.Pod {
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(namespace), client.MatchingLabels{"kdfs.krea.to/mode": "replica"}); err != nil {
		return nil
	}
	for i := range pods.Items {
		if pods.Items[i].Name == replicaName+"-pod" && pods.Items[i].Status.PodIP != "" {
			return &pods.Items[i]
		}
	}
	return nil
}

func findExistingHealth(healths []storagev1alpha1.ReplicaHealth, name string) *storagev1alpha1.ReplicaHealth {
	for i := range healths {
		if healths[i].Name == name {
			return &healths[i]
		}
	}
	return &storagev1alpha1.ReplicaHealth{}
}

func removeAttachment(attachments []storagev1alpha1.ReplicaAttachment, index int) []storagev1alpha1.ReplicaAttachment {
	return append(attachments[:index], attachments[index+1:]...)
}
