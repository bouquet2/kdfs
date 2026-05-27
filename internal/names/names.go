package names

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

const defaultNQNAuthority = "nqn.2026-05.krea.to"

func authority() string {
	value := strings.TrimSpace(os.Getenv("NQN_AUTHORITY"))
	if value == "" {
		return defaultNQNAuthority
	}
	return value
}

func hostNQNPattern() *regexp.Regexp {
	return regexp.MustCompile(`^` + regexp.QuoteMeta(authority()) + `:krea\.to:host-[a-z0-9.-]+$`)
}

func EngineName(volumeName string) string { return volumeName + "-engine" }

func ReplicaName(volumeName string, i int) string { return fmt.Sprintf("%s-replica-%d", volumeName, i) }

func DataPath(volumeName string) string { return fmt.Sprintf("/var/lib/kdfs/%s/vol.img", volumeName) }

func VolumeNQN(volumeName string) string { return fmt.Sprintf("%s:volume-%s", authority(), volumeName) }

func ReplicaNQN(volumeName string, i int) string {
	return fmt.Sprintf("%s:replica-%s-%d", authority(), volumeName, i)
}

func VolumeROXNQN(volumeName string) string { return fmt.Sprintf("%s:rox-%s", authority(), volumeName) }

func HostNQN(nodeName string) string { return fmt.Sprintf("%s:krea.to:host-%s", authority(), nodeName) }

func IsHostNQN(value string) bool { return hostNQNPattern().MatchString(value) }
