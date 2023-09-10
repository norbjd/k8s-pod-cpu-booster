package cgroup

import "k8s.io/apimachinery/pkg/types"

type CgroupHandler interface {
	WriteCPUMax(podUID types.UID, containerID string, cpuMaxValue uint64) error
}
