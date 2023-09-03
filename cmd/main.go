package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/informers/internalinterfaces"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

const (
	cpuBoostStartupLabel  = "norbjd/k8s-pod-cpu-booster-enabled"
	cpuBoostMultiplicator = 10
)

// Inspired by:
// - https://www.cncf.io/blog/2019/10/15/extend-kubernetes-via-a-shared-informer/
// TODO: split in multiple packages
func main() {
	klog.InitFlags(nil)
	flag.Parse()

	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	factory := informers.NewSharedInformerFactoryWithOptions(clientset, 0, informers.WithTweakListOptions(podCPUBoosterTweakFunc()))
	informer := factory.Core().V1().Pods().Informer()

	stopper := make(chan struct{})
	defer close(stopper)
	defer runtime.HandleCrash()

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: onUpdate,
	})

	go informer.Run(stopper)
	klog.Infof("Informer started!")

	if !cache.WaitForCacheSync(stopper, informer.HasSynced) {
		runtime.HandleError(errors.New("timed out waiting for caches to sync"))
		return
	}

	<-stopper
}

// only check pods running on the current node, assumes NODE_NAME contains the name of the node
func podCPUBoosterTweakFunc() internalinterfaces.TweakListOptionsFunc {
	return func(opts *metav1.ListOptions) {
		opts.FieldSelector = "spec.nodeName=" + os.Getenv("NODE_NAME")
	}
}

func onUpdate(oldObj interface{}, newObj interface{}) {
	oldPod := oldObj.(*corev1.Pod)
	newPod := newObj.(*corev1.Pod)
	klog.Infof(
		"pod %s/%s updated",
		oldPod.Namespace, oldPod.Name,
	)

	if podHasBoostLabel(newPod) {
		if len(newPod.Status.ContainerStatuses) == 0 {
			klog.Infof("pod %s/%s has no container statuses, skipping...", newPod.Namespace, newPod.Name)
			return
		}

		if len(newPod.Spec.Containers) != 1 {
			klog.Infof("pod %s/%s contains %d containers, skipping...", newPod.Namespace, newPod.Name, len(newPod.Spec.Containers))
			return
		}

		podUID := newPod.UID
		containerID := newPod.Status.ContainerStatuses[0].ContainerID // this is same to use [0] as we've checked above if we have 1 container

		initialCPULimit := newPod.Spec.Containers[0].Resources.Limits.Cpu()

		// once ready, we reset the CPU as the normal limit
		if podPassedFromNotReadyToReady(oldPod, newPod) {
			klog.Infof("will reset %s/%s CPU limit to default", newPod.Namespace, newPod.Name)
			err := resetCPUBoost(podUID, containerID, initialCPULimit)
			if err != nil {
				klog.Errorf("error while resetting CPU boost: %s", err.Error())
			}
		} else { // TODO: do that only once
			klog.Infof("will boost %s/%s CPU limit", newPod.Namespace, newPod.Name)
			err := boostCPU(podUID, containerID, initialCPULimit)
			if err != nil {
				klog.Errorf("error while boosting CPU: %s", err.Error())
			}
		}
	}
}

func podHasBoostLabel(pod *corev1.Pod) bool {
	boost, ok := pod.Labels[cpuBoostStartupLabel]
	return ok && boost == "true"
}

func podPassedFromNotReadyToReady(oldPod, newPod *corev1.Pod) bool {
	podWasNotReady := false

	for _, condition := range oldPod.Status.Conditions {
		if condition.Type == "Ready" && condition.Status == "False" {
			podWasNotReady = true
			break
		}
	}

	if podWasNotReady {
		for _, condition := range newPod.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				klog.Infof(
					"pod %s/%s is now ready",
					oldPod.Namespace, oldPod.Name,
				)
				return true
			}
		}
	}

	return false
}

func quantityToCPULimit(quantity *resource.Quantity) uint64 {
	return uint64(quantity.AsApproximateFloat64() * 100_000.)
}

func boostCPU(podUID types.UID, containerID string, initialCPULimit *resource.Quantity) error {
	klog.Infof("CPU limit: %d", quantityToCPULimit(initialCPULimit))
	cgroupCPULimitAfterBoost := quantityToCPULimit(initialCPULimit) * cpuBoostMultiplicator
	klog.Infof("New cgroup cpu limit to: %d", cgroupCPULimitAfterBoost)

	err := writeCgroupCPUMax(podUID, containerID, cgroupCPULimitAfterBoost)
	if err != nil {
		return err
	}

	return nil
}

func resetCPUBoost(podUID types.UID, containerID string, initialCPULimit *resource.Quantity) error {
	cgroupCPULimitAfterReset := quantityToCPULimit(initialCPULimit)
	klog.Infof("Reset cgroup cpu limit to: %d", cgroupCPULimitAfterReset)

	err := writeCgroupCPUMax(podUID, containerID, cgroupCPULimitAfterReset)
	if err != nil {
		return err
	}

	return nil
}

// TODO: works ONLY with kind + containerd + cgroup v2
// TODO: create an interface and implement with "Containerd cgroup v2", "Containerd cgroup v1", etc.
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

// TODO: write only if file exists. Try to understand when the file is available
func writeCgroupCPUMax(podUID types.UID, containerID string, newCPUMax uint64) error {
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
