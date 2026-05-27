package controller

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	storagev1alpha1 "github.com/bouquet2/kdfs/api/v1alpha1"
)

func replicasHash(replicas []storagev1alpha1.ReplicaAttachment) string {
	data, _ := json.Marshal(replicas)
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:8])
}
