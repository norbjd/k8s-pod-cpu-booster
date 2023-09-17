package cgroup

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/types"
)

type V1KindHandler struct{}

func (h V1KindHandler) GetVersion() version {
	return v1
}

func (h V1KindHandler) GetPodDirectory(podUID types.UID) string {
	return fmt.Sprintf(
		"/sys/fs/cgroup/cpu,cpuacct/kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-pod%s.slice",
		strings.ReplaceAll(string(podUID), "-", "_"),
	)
}

func (h V1KindHandler) GetContainerDirectory(podUID types.UID, containerID string) string {
	podDirectory := h.GetPodDirectory(podUID)
	containerID = strings.Replace(containerID, "containerd://", "", 1) // assumes only containerd
	return fmt.Sprintf(
		"%s/cri-containerd-%s.scope",
		podDirectory,
		containerID,
	)
}
