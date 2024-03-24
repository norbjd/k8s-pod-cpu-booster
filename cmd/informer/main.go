package main

import (
	"context"
	"flag"

	"github.com/norbjd/k8s-pod-cpu-booster/pkg/informer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

func main() {
	klog.InitFlags(nil)

	var id string
	var leaseLockNamespace string
	var leaseLockName string

	flag.StringVar(&id, "id", "", "the lease lock resource name")
	flag.StringVar(&leaseLockNamespace, "lease-lock-namespace", "", "the lease lock resource namespace")
	flag.StringVar(&leaseLockName, "lease-lock-name", "", "path to key file")

	flag.Parse()

	if id == "" {
		klog.Fatal("lease holder identity is required (missing id flag)")
	}
	if leaseLockNamespace == "" {
		klog.Fatal("unable to get lease lock resource namespace (missing lease-lock-namespace flag)")
	}
	if leaseLockName == "" {
		klog.Fatal("unable to get lease lock resource name (missing lease-lock-name flag)")
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	informer.Run(ctx, clientset, id, leaseLockNamespace, leaseLockName)
}
