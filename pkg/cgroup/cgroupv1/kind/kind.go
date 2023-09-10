package cgroupv1_containerd_kind

import (
	"fmt"
	"os"
	"path"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

type Cgroupv1KindHandler struct{}

// TODO: write only if file exists. Try to understand when the file is available
func (m Cgroupv1KindHandler) WriteCPUMax(podUID types.UID, containerID string, newCPUMax uint64) error {
	podCgroupSliceDirectory := getPodCgroupSliceDirectory(podUID)
	containerCgroupScopeDirectory := getContainerCgroupScopeDirectory(podCgroupSliceDirectory, containerID)

	podCgroupCPUCfsQuotaUsFile := path.Join(podCgroupSliceDirectory, "cpu.cfs_quota_us")
	containerCgroupCPUCfsQuotaUsFile := path.Join(containerCgroupScopeDirectory, "cpu.cfs_quota_us")

	newCPUCfsQuotaUsFileContents := fmt.Sprintf("%d", newCPUMax)

	klog.Infof("will write %s to %s and %s", newCPUCfsQuotaUsFileContents, podCgroupCPUCfsQuotaUsFile, containerCgroupCPUCfsQuotaUsFile)

	err := os.WriteFile(podCgroupCPUCfsQuotaUsFile, []byte(newCPUCfsQuotaUsFileContents), 0o644)
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", podCgroupCPUCfsQuotaUsFile, err)
	}

	err = os.WriteFile(containerCgroupCPUCfsQuotaUsFile, []byte(newCPUCfsQuotaUsFileContents), 0o644)
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", containerCgroupCPUCfsQuotaUsFile, err)
	}

	podCgroupCPUMaxFileContents, _ := os.ReadFile(podCgroupCPUCfsQuotaUsFile)
	klog.Infof("pod cpu.cfs_quota_us: %s", string(podCgroupCPUMaxFileContents))

	containerCgroupCPUMaxFileContents, _ := os.ReadFile(containerCgroupCPUCfsQuotaUsFile)
	klog.Infof("container cpu.cfs_quota_us: %s", string(containerCgroupCPUMaxFileContents))

	return nil
}

func getPodCgroupSliceDirectory(podUID types.UID) string {
	return fmt.Sprintf(
		"/sys/fs/cgroup/cpu/kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-pod%s.slice",
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
