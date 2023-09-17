package cgroup

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

func v1WriteCPUCfsQuotaUs(handler Handler, podUID types.UID, containerID string, newCfsQuotaUs uint64) error {
	if handler.GetVersion() != v1 {
		return fmt.Errorf("%w: handler must be %s, but got %s", errMismatchVersion, v1.String(), handler.GetVersion().String())
	}

	podDirectory := handler.GetPodDirectory(podUID)
	containerDirectory := handler.GetContainerDirectory(podUID, containerID)

	podCPUCfsQuotaUsFile := path.Join(podDirectory, "cpu.cfs_quota_us")
	containerCPUCfsQuotaUsFile := path.Join(containerDirectory, "cpu.cfs_quota_us")

	newCPUCfsQuotaUsFileContents := fmt.Sprintf("%d", newCfsQuotaUs)

	klog.Infof("will write %s to %s and %s", newCPUCfsQuotaUsFileContents, podCPUCfsQuotaUsFile, containerCPUCfsQuotaUsFile)

	podCurrentCPUCfsQuotaUs, err := os.ReadFile(podCPUCfsQuotaUsFile)
	if err != nil {
		return fmt.Errorf("cannot read pod current cgroup cpu quota %s: %w", podCPUCfsQuotaUsFile, err)
	}
	podCurrentCPUCfsQuotaUsValueString := strings.TrimSuffix(string(podCurrentCPUCfsQuotaUs), "\n")
	podCurrentCPUCfsQuotaUsValue, err := strconv.ParseInt(podCurrentCPUCfsQuotaUsValueString, 10, 64)
	if err != nil {
		return fmt.Errorf("cannot convert pod current cgroup cpu quota value %s in %s: %w",
			podCurrentCPUCfsQuotaUsValueString, podCPUCfsQuotaUsFile, err)
	}

	// note: for cgroup v1, the order to update files is important
	if int64(newCfsQuotaUs) >= podCurrentCPUCfsQuotaUsValue {
		klog.Infof("new cpu value (%d) is greater or equal than existing one (%s), so will update the pod cgroup first (%s), and then the container cgroup (%s)",
			newCfsQuotaUs, podCurrentCPUCfsQuotaUsValue, podCPUCfsQuotaUsFile, containerCPUCfsQuotaUsFile)

		err = os.WriteFile(podCPUCfsQuotaUsFile, []byte(newCPUCfsQuotaUsFileContents), 0o644)
		if err != nil {
			return fmt.Errorf("cannot write to %s: %w", podCPUCfsQuotaUsFile, err)
		}

		err = os.WriteFile(containerCPUCfsQuotaUsFile, []byte(newCPUCfsQuotaUsFileContents), 0o644)
		if err != nil {
			return fmt.Errorf("cannot write to %s: %w", containerCPUCfsQuotaUsFile, err)
		}
	} else {
		klog.Infof("new cpu value (%d) is smaller than existing one (%s), so will update the container cgroup first (%s), and then the pod cgroup (%s)",
			newCfsQuotaUs, podCurrentCPUCfsQuotaUsValue, containerCPUCfsQuotaUsFile, podCPUCfsQuotaUsFile)

		err = os.WriteFile(containerCPUCfsQuotaUsFile, []byte(newCPUCfsQuotaUsFileContents), 0o644)
		if err != nil {
			return fmt.Errorf("cannot write to %s: %w", containerCPUCfsQuotaUsFile, err)
		}

		err = os.WriteFile(podCPUCfsQuotaUsFile, []byte(newCPUCfsQuotaUsFileContents), 0o644)
		if err != nil {
			return fmt.Errorf("cannot write to %s: %w", podCPUCfsQuotaUsFile, err)
		}
	}

	podCPUCfsQuotaUsFileContents, _ := os.ReadFile(podCPUCfsQuotaUsFile)
	klog.Infof("pod cpu.cfs_quota_us: %s", string(podCPUCfsQuotaUsFileContents))

	containerCPUCfsQuotaUsFileContents, _ := os.ReadFile(containerCPUCfsQuotaUsFile)
	klog.Infof("container cpu.cfs_quota_us: %s", string(containerCPUCfsQuotaUsFileContents))

	return nil
}
