package cgroup

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/types"
)

type V2KapsuleHandler struct{}

func (h V2KapsuleHandler) GetVersion() version {
	return v2
}

func (h V2KapsuleHandler) GetPodDirectory(podUID types.UID) string {
	return fmt.Sprintf("/sys/fs/cgroup/kubepods/pod%s", podUID)
}

func (h V2KapsuleHandler) GetContainerDirectory(podUID types.UID, containerID string) string {
	podDirectory := h.GetPodDirectory(podUID)
	containerID = strings.Replace(containerID, "containerd://", "", 1) // assumes only containerd
	return fmt.Sprintf(
		"%s/%s",
		podDirectory,
		containerID,
	)
}
