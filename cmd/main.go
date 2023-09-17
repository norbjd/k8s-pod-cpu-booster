package main

import (
	"flag"
	"os"

	"github.com/norbjd/k8s-pod-cpu-booster/pkg/cgroup"
	cgroupv1_containerd_kapsule "github.com/norbjd/k8s-pod-cpu-booster/pkg/cgroup/cgroupv1/kapsule"
	cgroupv1_containerd_kind "github.com/norbjd/k8s-pod-cpu-booster/pkg/cgroup/cgroupv1/kind"
	cgroupv2_containerd_kapsule "github.com/norbjd/k8s-pod-cpu-booster/pkg/cgroup/cgroupv2/kapsule"
	cgroupv2_containerd_kind "github.com/norbjd/k8s-pod-cpu-booster/pkg/cgroup/cgroupv2/kind"
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
		switch os.Getenv("K8S_DISTRIBUTION") {
		case "kapsule":
			klog.Info("Using Cgroupv1KapsuleHandler")
			cgroupHandler = cgroupv1_containerd_kapsule.Cgroupv1KapsuleHandler{}
		default:
			klog.Info("Using Cgroupv1KindHandler")
			cgroupHandler = cgroupv1_containerd_kind.Cgroupv1KindHandler{}
		}
	default:
		switch os.Getenv("K8S_DISTRIBUTION") {
		case "kapsule":
			klog.Info("Using Cgroupv2KapsuleHandler")
			cgroupHandler = cgroupv2_containerd_kapsule.Cgroupv2KapsuleHandler{}
		default:
			klog.Info("Using Cgroupv2KindHandler")
			cgroupHandler = cgroupv2_containerd_kind.Cgroupv2KindHandler{}
		}
	}

	informer.Run(clientset, cgroupHandler)
}
