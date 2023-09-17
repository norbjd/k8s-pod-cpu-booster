package informer

import (
	"errors"
	"os"
	"strconv"

	"github.com/norbjd/k8s-pod-cpu-booster/pkg/cgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/informers/internalinterfaces"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

const (
	cpuBoostStartupAnnotation = "norbjd.github.io/k8s-pod-cpu-booster-enabled"

	cpuBoostMultiplierAnnotation = "norbjd.github.io/k8s-pod-cpu-booster-multiplier"
	cpuBoostDefaultMultiplier    = uint64(10)
)

// Inspired by:
// - https://www.cncf.io/blog/2019/10/15/extend-kubernetes-via-a-shared-informer/
func Run(clientset *kubernetes.Clientset, cgroupHandler cgroup.Handler) {
	factory := informers.NewSharedInformerFactoryWithOptions(clientset, 0, informers.WithTweakListOptions(podCPUBoosterTweakFunc()))
	informer := factory.Core().V1().Pods().Informer()

	stopper := make(chan struct{})
	defer close(stopper)
	defer runtime.HandleCrash()

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			onUpdate(cgroupHandler, oldObj, newObj)
		},
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

func getBoostMultiplierFromAnnotations(pod *corev1.Pod) uint64 {
	if boostMultiplierAnnotationValue, ok := pod.Annotations["cpuBoostMultiplierAnnotation"]; ok {
		boostMultiplierAnnotationValueInt, err := strconv.ParseUint(boostMultiplierAnnotationValue, 10, 64)
		if err != nil {
			klog.Errorf("boost multiplier is not a valid value, will take the default %d instead: %s",
				cpuBoostDefaultMultiplier, err.Error())
			return cpuBoostDefaultMultiplier
		}

		return boostMultiplierAnnotationValueInt
	}

	return cpuBoostDefaultMultiplier
}

func onUpdate(cgroupHandler cgroup.Handler, oldObj interface{}, newObj interface{}) {
	oldPod := oldObj.(*corev1.Pod)
	newPod := newObj.(*corev1.Pod)
	klog.Infof(
		"pod %s/%s updated",
		oldPod.Namespace, oldPod.Name,
	)

	if podHasBoostAnnotation(newPod) {
		if len(newPod.Status.ContainerStatuses) == 0 {
			klog.Infof("pod %s/%s has no container statuses, skipping...", newPod.Namespace, newPod.Name)
			return
		}

		if len(newPod.Spec.Containers) != 1 {
			klog.Infof("pod %s/%s contains %d containers, skipping...", newPod.Namespace, newPod.Name, len(newPod.Spec.Containers))
			return
		}

		podUID := newPod.UID
		containerID := newPod.Status.ContainerStatuses[0].ContainerID // this is safe to use [0] as we've checked above if we have 1 container

		initialCPULimit := newPod.Spec.Containers[0].Resources.Limits.Cpu()
		boostMultiplier := getBoostMultiplierFromAnnotations(newPod)

		// once ready, we reset the CPU as the normal limit
		if podPassedFromNotReadyToReady(oldPod, newPod) {
			klog.Infof("will reset %s/%s CPU limit to default", newPod.Namespace, newPod.Name)
			err := resetCPUBoost(cgroupHandler, podUID, containerID, initialCPULimit)
			if err != nil {
				klog.Errorf("error while resetting CPU boost: %s", err.Error())
			}
		} else { // TODO: do that only once
			klog.Infof("will boost %s/%s CPU limit", newPod.Namespace, newPod.Name)
			err := boostCPU(cgroupHandler, podUID, containerID, initialCPULimit, boostMultiplier)
			if err != nil {
				klog.Errorf("error while boosting CPU: %s", err.Error())
			}
		}
	}
}

func podHasBoostAnnotation(pod *corev1.Pod) bool {
	boost, ok := pod.Annotations[cpuBoostStartupAnnotation]
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

func boostCPU(cgroupHandler cgroup.Handler, podUID types.UID, containerID string, initialCPULimit *resource.Quantity, boostMultiplier uint64) error {
	klog.Infof("CPU limit: %d", quantityToCPULimit(initialCPULimit))
	cgroupCPULimitAfterBoost := quantityToCPULimit(initialCPULimit) * boostMultiplier
	klog.Infof("New cgroup cpu limit to: %d", cgroupCPULimitAfterBoost)

	err := cgroup.WriteCPULimit(cgroupHandler, podUID, containerID, cgroupCPULimitAfterBoost)
	if err != nil {
		return err
	}

	return nil
}

func resetCPUBoost(cgroupHandler cgroup.Handler, podUID types.UID, containerID string, initialCPULimit *resource.Quantity) error {
	cgroupCPULimitAfterReset := quantityToCPULimit(initialCPULimit)
	klog.Infof("Reset cgroup cpu limit to: %d", cgroupCPULimitAfterReset)

	err := cgroup.WriteCPULimit(cgroupHandler, podUID, containerID, cgroupCPULimitAfterReset)
	if err != nil {
		return err
	}

	return nil
}
