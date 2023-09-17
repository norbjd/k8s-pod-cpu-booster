package cgroupv1_containerd_kapsule

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

type Cgroupv1KapsuleHandler struct{}

// TODO: write only if file exists. Try to understand when the file is available
// TODO: same as Cgroupv1KindHandler, extract into common method and reuse
func (m Cgroupv1KapsuleHandler) WriteCPUMax(podUID types.UID, containerID string, newCPUMax uint64) error {
	podCgroupSliceDirectory := getPodCgroupSliceDirectory(podUID)
	containerCgroupScopeDirectory := getContainerCgroupScopeDirectory(podCgroupSliceDirectory, containerID)

	podCgroupCPUCfsQuotaUsFile := path.Join(podCgroupSliceDirectory, "cpu.cfs_quota_us")
	containerCgroupCPUCfsQuotaUsFile := path.Join(containerCgroupScopeDirectory, "cpu.cfs_quota_us")

	newCPUCfsQuotaUsFileContents := fmt.Sprintf("%d", newCPUMax)

	klog.Infof("will write %s to %s and %s", newCPUCfsQuotaUsFileContents, podCgroupCPUCfsQuotaUsFile, containerCgroupCPUCfsQuotaUsFile)

	podCgroupCurrentCPUCfsQuotaUs, err := os.ReadFile(podCgroupCPUCfsQuotaUsFile)
	if err != nil {
		return fmt.Errorf("cannot read pod current cgroup cpu quota %s: %w", podCgroupCPUCfsQuotaUsFile, err)
	}
	podCgroupCurrentCPUCfsQuotaUsValueString := strings.TrimSuffix(string(podCgroupCurrentCPUCfsQuotaUs), "\n")
	podCgroupCurrentCPUCfsQuotaUsValue, err := strconv.ParseInt(podCgroupCurrentCPUCfsQuotaUsValueString, 10, 64)
	if err != nil {
		return fmt.Errorf("cannot convert pod current cgroup cpu quota value %s in %s: %w",
			podCgroupCurrentCPUCfsQuotaUsValueString, podCgroupCPUCfsQuotaUsFile, err)
	}

	// note: for cgroup v1, the order to update files is important
	if int64(newCPUMax) >= podCgroupCurrentCPUCfsQuotaUsValue {
		klog.Infof("new cpu value (%d) is greater or equal than existing one (%s), so will update the pod cgroup first (%s), and then the container cgroup (%s)",
			newCPUMax, podCgroupCurrentCPUCfsQuotaUsValue, podCgroupCPUCfsQuotaUsFile, containerCgroupCPUCfsQuotaUsFile)

		err = os.WriteFile(podCgroupCPUCfsQuotaUsFile, []byte(newCPUCfsQuotaUsFileContents), 0o644)
		if err != nil {
			return fmt.Errorf("cannot write to %s: %w", podCgroupCPUCfsQuotaUsFile, err)
		}

		err = os.WriteFile(containerCgroupCPUCfsQuotaUsFile, []byte(newCPUCfsQuotaUsFileContents), 0o644)
		if err != nil {
			return fmt.Errorf("cannot write to %s: %w", containerCgroupCPUCfsQuotaUsFile, err)
		}
	} else {
		klog.Infof("new cpu value (%d) is smaller than existing one (%s), so will update the container cgroup first (%s), and then the pod cgroup (%s)",
			newCPUMax, podCgroupCurrentCPUCfsQuotaUsValue, containerCgroupCPUCfsQuotaUsFile, podCgroupCPUCfsQuotaUsFile)

		err = os.WriteFile(containerCgroupCPUCfsQuotaUsFile, []byte(newCPUCfsQuotaUsFileContents), 0o644)
		if err != nil {
			return fmt.Errorf("cannot write to %s: %w", containerCgroupCPUCfsQuotaUsFile, err)
		}

		err = os.WriteFile(podCgroupCPUCfsQuotaUsFile, []byte(newCPUCfsQuotaUsFileContents), 0o644)
		if err != nil {
			return fmt.Errorf("cannot write to %s: %w", podCgroupCPUCfsQuotaUsFile, err)
		}
	}

	podCgroupCPUMaxFileContents, _ := os.ReadFile(podCgroupCPUCfsQuotaUsFile)
	klog.Infof("pod cpu.cfs_quota_us: %s", string(podCgroupCPUMaxFileContents))

	containerCgroupCPUMaxFileContents, _ := os.ReadFile(containerCgroupCPUCfsQuotaUsFile)
	klog.Infof("container cpu.cfs_quota_us: %s", string(containerCgroupCPUMaxFileContents))

	return nil
}

func getPodCgroupSliceDirectory(podUID types.UID) string {
	return fmt.Sprintf("/sys/fs/cgroup/cpu,cpuacct/kubepods/pod%s", podUID)
}

func getContainerCgroupScopeDirectory(podCgroupSliceDirectory, containerID string) string {
	containerID = strings.Replace(containerID, "containerd://", "", 1) // assumes only containerd
	return fmt.Sprintf(
		"%s/%s",
		podCgroupSliceDirectory,
		containerID,
	)
}
