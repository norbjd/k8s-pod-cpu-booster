package cgroup

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/types"
)

type V1KapsuleHandler struct{}

func (h V1KapsuleHandler) GetVersion() version {
	return v1
}

func (h V1KapsuleHandler) GetPodDirectory(podUID types.UID) string {
	return fmt.Sprintf("/sys/fs/cgroup/cpu,cpuacct/kubepods/pod%s", string(podUID))
}

func (h V1KapsuleHandler) GetContainerDirectory(podUID types.UID, containerID string) string {
	podDirectory := h.GetPodDirectory(podUID)
	containerID = strings.Replace(containerID, "containerd://", "", 1) // assumes only containerd
	return fmt.Sprintf(
		"%s/%s",
		podDirectory,
		containerID,
	)
}
