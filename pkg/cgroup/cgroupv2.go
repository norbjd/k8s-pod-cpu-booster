package cgroup

import (
	"fmt"
	"os"
	"path"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

func v2WriteCPUMax(handler Handler, podUID types.UID, containerID string, newCPUMax uint64) error {
	if handler.GetVersion() != v2 {
		return fmt.Errorf("%w: handler must be %s, but got %s", errMismatchVersion, v2.String(), handler.GetVersion().String())
	}

	podCgroupSliceDirectory := handler.GetPodDirectory(podUID)
	containerCgroupScopeDirectory := handler.GetContainerDirectory(podUID, containerID)

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
