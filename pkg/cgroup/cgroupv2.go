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

	podDirectory := handler.GetPodDirectory(podUID)
	containerDirectory := handler.GetContainerDirectory(podUID, containerID)

	podCPUMaxFile := path.Join(podDirectory, "cpu.max")
	containerCPUMaxFile := path.Join(containerDirectory, "cpu.max")

	newCPUMaxFileContents := fmt.Sprintf("%d 100000", newCPUMax)

	klog.Infof("will write %s to %s and %s", newCPUMaxFileContents, podCPUMaxFile, containerCPUMaxFile)

	err := os.WriteFile(podCPUMaxFile, []byte(newCPUMaxFileContents), 0o644)
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", podCPUMaxFile, err)
	}

	err = os.WriteFile(containerCPUMaxFile, []byte(newCPUMaxFileContents), 0o644)
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", containerCPUMaxFile, err)
	}

	podCPUMaxFileContents, _ := os.ReadFile(podCPUMaxFile)
	klog.Infof("pod cpu.max: %s", string(podCPUMaxFileContents))

	containerCPUMaxFileContents, _ := os.ReadFile(containerCPUMaxFile)
	klog.Infof("container cpu.max: %s", string(containerCPUMaxFileContents))

	return nil
}
