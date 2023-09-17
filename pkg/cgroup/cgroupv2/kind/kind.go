package cgroupv2_containerd_kind

import (
	"fmt"
	"os"
	"path"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

type Cgroupv2KindHandler struct{}

// TODO: write only if file exists. Try to understand when the file is available
func (m Cgroupv2KindHandler) WriteCPUMax(podUID types.UID, containerID string, newCPUMax uint64) error {
	podCgroupSliceDirectory := getPodCgroupSliceDirectory(podUID)
	containerCgroupScopeDirectory := getContainerCgroupScopeDirectory(podCgroupSliceDirectory, containerID)

	podCgroupCPUMaxFile := path.Join(podCgroupSliceDirectory, "cpu.max")
	containerCgroupCPUMaxFile := path.Join(containerCgroupScopeDirectory, "cpu.max")

	newCPUMaxFileContents := fmt.Sprintf("%d 100000", newCPUMax)

	klog.Infof("will write %s to %s and %s", newCPUMaxFileContents, podCgroupCPUMaxFile, containerCgroupCPUMaxFile)

	err := os.WriteFile(podCgroupCPUMaxFile, []byte(newCPUMaxFileContents), 0o644)
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", podCgroupCPUMaxFile, err)
	}

	err = os.WriteFile(containerCgroupCPUMaxFile, []byte(newCPUMaxFileContents), 0o644)
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", containerCgroupCPUMaxFile, err)
	}

	podCgroupCPUMaxFileContents, _ := os.ReadFile(podCgroupCPUMaxFile)
	klog.Infof("pod cpu.max: %s", string(podCgroupCPUMaxFileContents))

	containerCgroupCPUMaxFileContents, _ := os.ReadFile(containerCgroupCPUMaxFile)
	klog.Infof("container cpu.max: %s", string(containerCgroupCPUMaxFileContents))

	return nil
}

func getPodCgroupSliceDirectory(podUID types.UID) string {
	return fmt.Sprintf(
		"/sys/fs/cgroup/kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-pod%s.slice",
		strings.ReplaceAll(string(podUID), "-", "_"),
	)
}

func getContainerCgroupScopeDirectory(podCgroupSliceDirectory, containerID string) string {
	containerID = strings.Replace(containerID, "containerd://", "", 1) // assumes only containerd
	return fmt.Sprintf(
		"%s/cri-containerd-%s.scope",
		podCgroupSliceDirectory,
		containerID,
	)
}
