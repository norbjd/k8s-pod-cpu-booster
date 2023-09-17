package main

import (
	"flag"
	"os"

	"github.com/norbjd/k8s-pod-cpu-booster/pkg/cgroup"
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

	var cgroupHandler cgroup.Handler

	// TODO: instead of using an env var, detect automatically the right cgroup handler to use
	switch os.Getenv("CGROUP_VERSION") {
	case "v1":
		switch os.Getenv("K8S_DISTRIBUTION") {
		case "kapsule":
			klog.Info("Using V1KapsuleHandler")
			cgroupHandler = cgroup.V1KapsuleHandler{}
		default:
			klog.Info("Using V1KindHandler")
			cgroupHandler = cgroup.V1KindHandler{}
		}
	default:
		switch os.Getenv("K8S_DISTRIBUTION") {
		case "kapsule":
			klog.Info("Using V2KapsuleHandler")
			cgroupHandler = cgroup.V2KapsuleHandler{}
		default:
			klog.Info("Using V2KindHandler")
			cgroupHandler = cgroup.V2KindHandler{}
		}
	}

	informer.Run(clientset, cgroupHandler)
}
