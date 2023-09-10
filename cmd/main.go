package main

import (
	"flag"

	cgroupv1_containerd_kind "github.com/norbjd/k8s-pod-cpu-booster/pkg/cgroup/cgroupv1/containerd/kind"
	"github.com/norbjd/k8s-pod-cpu-booster/pkg/informer"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

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

	// TODO: detect automatically the right cgroup handler to use
	// only support Kind + containerd + cgroups v2 for now
	cgroupHandler := cgroupv1_containerd_kind.Cgroupv1ContainerdKindHandler{}

	informer.Run(clientset, cgroupHandler)
}
