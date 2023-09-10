package main

import (
	"flag"
	"os"

	"github.com/norbjd/k8s-pod-cpu-booster/pkg/cgroup"
	cgroupv1_containerd_kind "github.com/norbjd/k8s-pod-cpu-booster/pkg/cgroup/cgroupv1/containerd/kind"
	cgroupv2_containerd_kind "github.com/norbjd/k8s-pod-cpu-booster/pkg/cgroup/cgroupv2/containerd/kind"
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

	var cgroupHandler cgroup.CgroupHandler

	// TODO: instead of using an env var, detect automatically the right cgroup handler to use
	switch os.Getenv("CGROUP_VERSION") {
	case "v1":
		cgroupHandler = cgroupv1_containerd_kind.Cgroupv1ContainerdKindHandler{}
	default:
		cgroupHandler = cgroupv2_containerd_kind.Cgroupv2ContainerdKindHandler{}
	}

	informer.Run(clientset, cgroupHandler)
}
