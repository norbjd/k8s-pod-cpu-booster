package cgroup

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
)

type version int

const (
	v1 version = iota
	v2
)

func (v version) String() string {
	switch v {
	case v1:
		return "v1"
	case v2:
		return "v2"
	default:
		return fmt.Sprintf("unknown: %d", v)
	}
}

type Handler interface {
	GetVersion() version
	GetPodDirectory(podUID types.UID) string
	GetContainerDirectory(podUID types.UID, containerID string) string
}

var (
	errInvalidVersion  = errors.New("invalid cgroup version")
	errMismatchVersion = errors.New("cgroup version mismatch")
)

func WriteCPULimit(handler Handler, podUID types.UID, containerID string, cpuLimit uint64) error {
	switch handler.GetVersion() {
	case v1:
		return v1WriteCPUCfsQuotaUs(handler, podUID, containerID, cpuLimit)
	case v2:
		return v2WriteCPUMax(handler, podUID, containerID, cpuLimit)
	default:
		return errInvalidVersion
	}
}
