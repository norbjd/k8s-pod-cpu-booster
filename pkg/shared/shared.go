package shared

import (
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

const (
	cpuBoostMultiplierLabel    = "norbjd.github.io/k8s-pod-cpu-booster-multiplier"
	cpuBoostDefaultMultiplier  = uint64(10)
	cpuBoostContainerNameLabel = "norbjd.github.io/k8s-pod-cpu-booster-container"
)

type BoostInfo struct {
	ContainerIndex int
	ContainerName  string
	Multiplier     uint64
}

func RetrieveBoostInfo(pod *corev1.Pod) (BoostInfo, error) {
	containerIndex, containerName, err := getContainerToBoost(pod)
	if err != nil {
		return BoostInfo{}, err
	}

	boostMultiplier := getBoostMultiplierFromLabels(pod)

	return BoostInfo{
		ContainerIndex: containerIndex,
		ContainerName:  containerName,
		Multiplier:     boostMultiplier,
	}, nil
}

func getBoostMultiplierFromLabels(pod *corev1.Pod) uint64 {
	if boostMultiplierLabelValue, ok := pod.Labels[cpuBoostMultiplierLabel]; ok {
		boostMultiplierLabelValueInt, err := strconv.ParseUint(boostMultiplierLabelValue, 10, 64)
		if err != nil {
			klog.Warningf("boost multiplier is not a valid value, will take the default %d instead: %s",
				cpuBoostDefaultMultiplier, err.Error())
			return cpuBoostDefaultMultiplier
		}

		return boostMultiplierLabelValueInt
	}

	return cpuBoostDefaultMultiplier
}

func getContainerToBoost(pod *corev1.Pod) (index int, name string, err error) {
	containerNameToBoost := pod.Labels[cpuBoostContainerNameLabel]
	containerIndex := -1

	if containerNameToBoost == "" {
		if len(pod.Spec.Containers) > 1 {
			return 0, "", fmt.Errorf("pod %s/%s contains %d containers but label %s is unset",
				pod.Namespace, pod.Name, len(pod.Spec.Containers), cpuBoostContainerNameLabel)
		} else {
			containerIndex = 0
		}
	} else {
		for i, container := range pod.Spec.Containers {
			if container.Name == containerNameToBoost {
				containerIndex = i
				break
			}
		}

		if containerIndex == -1 {
			return 0, "", fmt.Errorf("pod %s/%s contains no containers named %s (found in label %s)",
				pod.Namespace, pod.Name, containerNameToBoost, cpuBoostContainerNameLabel)
		}
	}

	return containerIndex, containerNameToBoost, nil
}
