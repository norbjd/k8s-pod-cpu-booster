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

		if len(newPod.Spec.Containers) != 1 {
			klog.Infof("pod %s/%s contains %d containers, skipping...", newPod.Namespace, newPod.Name, len(newPod.Spec.Containers))
			return
		}

		currentCPULimit := newPod.Spec.Containers[0].Resources.Limits.Cpu()
		boostMultiplier := getBoostMultiplierFromAnnotations(newPod)

		if podJustStartedAndNotReadyYet(newPod) {
			klog.Infof("will boost %s/%s CPU limit (currently: %s)", newPod.Namespace, newPod.Name, currentCPULimit)
			err := boostCPU(clientset, newPod, currentCPULimit, boostMultiplier)
			if err != nil {
				klog.Errorf("error while boosting CPU: %s", err.Error())
			}
		} else if podIsNowReadyAfterBoosting(newPod) {
			klog.Infof("will reset %s/%s CPU limit to default (currently: %s)", newPod.Namespace, newPod.Name, currentCPULimit)
			err := resetCPUBoost(clientset, newPod, currentCPULimit, boostMultiplier)
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

func boostCPU(clientset *kubernetes.Clientset, pod *corev1.Pod, currentCPULimit *resource.Quantity, boostMultiplier uint64) error {
	cpuLimitAfterBoost := resource.NewScaledQuantity(currentCPULimit.ScaledValue(resource.Nano)*int64(boostMultiplier), resource.Nano)
	klog.Infof("Will set new CPU limit to: %s", cpuLimitAfterBoost)

	err := writeCPULimit(clientset, pod, cpuLimitAfterBoost, boost)
	if err != nil {
		return err
	}

	return nil
}

func resetCPUBoost(clientset *kubernetes.Clientset, pod *corev1.Pod, currentCPULimit *resource.Quantity, boostMultiplier uint64) error {
	cpuLimitAfterReset := resource.NewScaledQuantity(currentCPULimit.ScaledValue(resource.Nano)/int64(boostMultiplier), resource.Nano)
	klog.Infof("Will reset CPU limit to: %s", cpuLimitAfterReset)

	err := writeCPULimit(clientset, pod, cpuLimitAfterReset, reset)
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

func writeCPULimit(clientset *kubernetes.Clientset, pod *corev1.Pod, cpuLimit *resource.Quantity, action action) error {
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

		newResources := corev1.ResourceRequirements{
			Requests: make(corev1.ResourceList),
			Limits:   make(corev1.ResourceList),
			Claims:   result.Spec.Containers[0].Resources.Claims,
		}

		for resourceName, resourceQuantity := range result.Spec.Containers[0].Resources.Requests {
			newResources.Requests[resourceName] = resourceQuantity
		}

		for resourceName, resourceQuantity := range result.Spec.Containers[0].Resources.Limits {
			newResources.Limits[resourceName] = resourceQuantity
		}

		newResources.Requests[corev1.ResourceCPU] = *cpuLimit
		newResources.Limits[corev1.ResourceCPU] = *cpuLimit

		result.Spec.Containers[0].Resources = newResources

		updatedPod, updateErr := podsClient.Update(ctx, result, metav1.UpdateOptions{})
		if updateErr != nil {
			return updateErr
		}

		klog.Infof("CPU request/limit successfully updated to %s/%s",
			updatedPod.Spec.Containers[0].Resources.Requests.Cpu(),
			updatedPod.Spec.Containers[0].Resources.Limits.Cpu(),
		)
		return nil
	})

	return retryErr
}
