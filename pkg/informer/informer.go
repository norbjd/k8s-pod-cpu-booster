package informer

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/norbjd/k8s-pod-cpu-booster/pkg/shared"
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
	cpuBoostStartupLabel         = "norbjd.github.io/k8s-pod-cpu-booster-enabled"
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

// only check pods with the CPU boost label set
func podCPUBoosterTweakFunc() internalinterfaces.TweakListOptionsFunc {
	return func(opts *metav1.ListOptions) {
		opts.LabelSelector = metav1.FormatLabelSelector(&metav1.LabelSelector{
			MatchLabels: map[string]string{
				cpuBoostStartupLabel: "true",
			},
		})
	}
}

func onUpdate(clientset *kubernetes.Clientset, oldObj interface{}, newObj interface{}) {
	oldPod := oldObj.(*corev1.Pod)
	newPod := newObj.(*corev1.Pod)
	klog.Infof("pod %s/%s updated", newPod.Namespace, newPod.Name)
	klog.V(9).Info(cmp.Diff(oldPod, newPod))

	if len(newPod.Status.ContainerStatuses) == 0 {
		klog.Infof("pod %s/%s has no container statuses, skipping...", newPod.Namespace, newPod.Name)
		return
	}

	boostInfo, err := shared.RetrieveBoostInfo(newPod)
	if err != nil {
		klog.ErrorS(err, "cannot retrieve boost info")
		return
	}

	if podIsNowReadyAfterBoosting(newPod) {
		klog.Infof("will reset %s/%s (container %s) CPU limit to default", newPod.Namespace, newPod.Name, boostInfo.ContainerName)
		err := resetCPUBoost(clientset, newPod, boostInfo.ContainerIndex, boostInfo.Multiplier)
		if err != nil {
			klog.Errorf("error while resetting CPU boost: %s", err.Error())
		}
	}
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

func resetCPUBoost(clientset *kubernetes.Clientset, pod *corev1.Pod, containerIndex int, boostMultiplier uint64) error {
	container := pod.Spec.Containers[containerIndex]
	currentCPULimit := container.Resources.Limits.Cpu()
	cpuLimitAfterReset := resource.NewScaledQuantity(currentCPULimit.ScaledValue(resource.Nano)/int64(boostMultiplier), resource.Nano)

	klog.Infof("Current CPU limit for %s/%s (container %s) is %s, will reset CPU limit to %s",
		pod.Namespace, pod.Name, container.Name, currentCPULimit, cpuLimitAfterReset)

	err := writeCPULimit(clientset, pod, containerIndex, cpuLimitAfterReset)
	if err != nil {
		return err
	}

	return nil
}

func writeCPULimit(clientset *kubernetes.Clientset, pod *corev1.Pod, containerIndex int, cpuLimit *resource.Quantity) error {
	ctx := context.Background()
	podsClient := clientset.CoreV1().Pods(pod.Namespace)

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, getErr := podsClient.Get(ctx, pod.Name, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("failed to get latest version of pod %s/%s: %v", pod.Namespace, pod.Name, getErr)
		}

		result.Labels[cpuBoostProgressLabelName] = cpuBoostDoneLabelValue

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
