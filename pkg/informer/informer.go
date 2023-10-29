package informer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/informers/internalinterfaces"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
)

const (
	cpuBoostStartupAnnotation = "norbjd.github.io/k8s-pod-cpu-booster-enabled"

	cpuBoostMultiplierAnnotation = "norbjd.github.io/k8s-pod-cpu-booster-multiplier"
	cpuBoostDefaultMultiplier    = uint64(10)

	cpuBoostContainerNameAnnotation = "norbjd.github.io/k8s-pod-cpu-booster-container"

	cpuBoostProgressLabelName    = "norbjd.github.io/k8s-pod-cpu-booster-progress"
	cpuBoostInProgressLabelValue = "boosting"
	cpuBoostDoneLabelValue       = "has-been-boosted"
)

// Inspired by:
// - https://www.cncf.io/blog/2019/10/15/extend-kubernetes-via-a-shared-informer/
func Run(clientset *kubernetes.Clientset) {
	factory := informers.NewSharedInformerFactoryWithOptions(clientset, 0, informers.WithTweakListOptions(podCPUBoosterTweakFunc()))
	informer := factory.Core().V1().Pods().Informer()

	stopper := make(chan struct{})
	defer close(stopper)
	defer runtime.HandleCrash()

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			onUpdate(clientset, oldObj, newObj)
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
// only necessary if we want to deploy pod-cpu-booster as a DaemonSet, otherwise a simple Deployment with 1 replica (to avoid conflicts) would be enough
func podCPUBoosterTweakFunc() internalinterfaces.TweakListOptionsFunc {
	return func(opts *metav1.ListOptions) {
		opts.FieldSelector = "spec.nodeName=" + os.Getenv("NODE_NAME")
	}
}

func getBoostMultiplierFromAnnotations(pod *corev1.Pod) uint64 {
	if boostMultiplierAnnotationValue, ok := pod.Annotations[cpuBoostMultiplierAnnotation]; ok {
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

func onUpdate(clientset *kubernetes.Clientset, oldObj interface{}, newObj interface{}) {
	oldPod := oldObj.(*corev1.Pod)
	newPod := newObj.(*corev1.Pod)
	klog.Infof("pod %s/%s updated", newPod.Namespace, newPod.Name)
	klog.V(9).Info(cmp.Diff(oldPod, newPod))

	if podHasBoostAnnotation(newPod) {
		if len(newPod.Status.ContainerStatuses) == 0 {
			klog.Infof("pod %s/%s has no container statuses, skipping...", newPod.Namespace, newPod.Name)
			return
		}

		containerNameToBoost := newPod.Annotations[cpuBoostContainerNameAnnotation]

		containerIndex := -1

		if containerNameToBoost == "" {
			if len(newPod.Spec.Containers) > 1 {
				klog.Warningf("pod %s/%s contains %d containers but annotation %s is unset, skipping...",
					newPod.Namespace, newPod.Name, len(newPod.Spec.Containers), cpuBoostContainerNameAnnotation)
				return
			} else {
				containerIndex = 0
			}
		} else {
			for i, container := range newPod.Spec.Containers {
				if container.Name == containerNameToBoost {
					containerIndex = i
					break
				}
			}

			if containerIndex == -1 {
				klog.Warningf("pod %s/%s contains no containers named %s (found in annotation %s), skipping...",
					newPod.Namespace, newPod.Name, containerNameToBoost, cpuBoostContainerNameAnnotation)
				return
			}
		}

		boostMultiplier := getBoostMultiplierFromAnnotations(newPod)

		if podJustStartedAndNotReadyYet(newPod) {
			klog.Infof("will boost %s/%s (container %s) CPU limit", newPod.Namespace, newPod.Name, containerNameToBoost)
			err := boostCPU(clientset, newPod, containerIndex, boostMultiplier)
			if err != nil {
				klog.Errorf("error while boosting CPU: %s", err.Error())
			}
		} else if podIsNowReadyAfterBoosting(newPod) {
			klog.Infof("will reset %s/%s (container %s) CPU limit to default", newPod.Namespace, newPod.Name, containerNameToBoost)
			err := resetCPUBoost(clientset, newPod, containerIndex, boostMultiplier)
			if err != nil {
				klog.Errorf("error while resetting CPU boost: %s", err.Error())
			}
		}
	}
}

func podHasBoostAnnotation(pod *corev1.Pod) bool {
	boost, ok := pod.Annotations[cpuBoostStartupAnnotation]
	return ok && boost == "true"
}

func podIsNowReadyAfterBoosting(newPod *corev1.Pod) bool {
	for _, condition := range newPod.Status.Conditions {
		if condition.Type == "Ready" && condition.Status == "True" {
			if newPod.Labels[cpuBoostProgressLabelName] == cpuBoostInProgressLabelValue {
				klog.Infof(
					"pod %s/%s is now ready after boost: %v",
					newPod.Namespace, newPod.Name, condition,
				)
				return true
			}
		}
	}

	return false
}

func podJustStartedAndNotReadyYet(pod *corev1.Pod) bool {
	// we have to wait until it's running before changing the CPU otherwise the behavior is undefined (caught this by experimenting)
	return pod.Status.Phase == corev1.PodRunning && pod.Labels[cpuBoostProgressLabelName] == ""
}

func boostCPU(clientset *kubernetes.Clientset, pod *corev1.Pod, containerIndex int, boostMultiplier uint64) error {
	container := pod.Spec.Containers[containerIndex]
	currentCPULimit := container.Resources.Limits.Cpu()
	cpuLimitAfterBoost := resource.NewScaledQuantity(currentCPULimit.ScaledValue(resource.Nano)*int64(boostMultiplier), resource.Nano)

	klog.Infof("Current CPU limit for %s/%s (container %s) is %s, will set new CPU limit to %s",
		pod.Namespace, pod.Name, container.Name, currentCPULimit, cpuLimitAfterBoost)

	err := writeCPULimit(clientset, pod, containerIndex, cpuLimitAfterBoost, boost)
	if err != nil {
		return err
	}

	return nil
}

func resetCPUBoost(clientset *kubernetes.Clientset, pod *corev1.Pod, containerIndex int, boostMultiplier uint64) error {
	container := pod.Spec.Containers[containerIndex]
	currentCPULimit := container.Resources.Limits.Cpu()
	cpuLimitAfterReset := resource.NewScaledQuantity(currentCPULimit.ScaledValue(resource.Nano)/int64(boostMultiplier), resource.Nano)

	klog.Infof("Current CPU limit for %s/%s (container %s) is %s, will reset CPU limit to %s",
		pod.Namespace, pod.Name, container.Name, currentCPULimit, cpuLimitAfterReset)

	err := writeCPULimit(clientset, pod, containerIndex, cpuLimitAfterReset, reset)
	if err != nil {
		return err
	}

	return nil
}

type action int32

const (
	boost action = iota
	reset
)

func writeCPULimit(clientset *kubernetes.Clientset, pod *corev1.Pod, containerIndex int, cpuLimit *resource.Quantity, action action) error {
	ctx := context.Background()
	podsClient := clientset.CoreV1().Pods(pod.Namespace)

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, getErr := podsClient.Get(ctx, pod.Name, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("failed to get latest version of pod %s/%s: %v", pod.Namespace, pod.Name, getErr)
		}

		if action == boost && result.Labels[cpuBoostProgressLabelName] == cpuBoostInProgressLabelValue {
			klog.Info("Already in boosting process, skipping...")
			return nil
		}

		switch action {
		case boost:
			if result.Labels == nil {
				result.Labels = make(map[string]string)
			}
			result.Labels[cpuBoostProgressLabelName] = cpuBoostInProgressLabelValue
		case reset:
			if result.Labels == nil {
				result.Labels = make(map[string]string)
			}
			result.Labels[cpuBoostProgressLabelName] = cpuBoostDoneLabelValue
		default:
			return fmt.Errorf("unknown action: %d (expected %d or %d)", action, boost, reset)
		}

		container := result.Spec.Containers[containerIndex]

		newResources := corev1.ResourceRequirements{
			Requests: make(corev1.ResourceList),
			Limits:   make(corev1.ResourceList),
			Claims:   container.Resources.Claims,
		}

		for resourceName, resourceQuantity := range container.Resources.Requests {
			newResources.Requests[resourceName] = resourceQuantity
		}

		for resourceName, resourceQuantity := range container.Resources.Limits {
			newResources.Limits[resourceName] = resourceQuantity
		}

		newResources.Requests[corev1.ResourceCPU] = *cpuLimit
		newResources.Limits[corev1.ResourceCPU] = *cpuLimit

		container.Resources = newResources

		result.Spec.Containers[containerIndex] = container

		updatedPod, updateErr := podsClient.Update(ctx, result, metav1.UpdateOptions{})
		if updateErr != nil {
			return updateErr
		}

		klog.Infof("CPU request/limit for %s/%s (container %s) successfully updated to %s/%s",
			updatedPod.Namespace,
			updatedPod.Name,
			container.Name,
			updatedPod.Spec.Containers[containerIndex].Resources.Requests.Cpu(),
			updatedPod.Spec.Containers[containerIndex].Resources.Limits.Cpu(),
		)
		return nil
	})

	return retryErr
}
