package main

import (
	"flag"

	"github.com/norbjd/k8s-pod-cpu-booster/pkg/webhook"
	"k8s.io/klog/v2"
)

func main() {
	klog.InitFlags(nil)

	var port uint
	var pathToCertFile string
	var pathToKeyFile string

	flag.UintVar(&port, "port", 8443, "listening port")
	flag.StringVar(&pathToCertFile, "cert", "", "path to cert file")
	flag.StringVar(&pathToKeyFile, "key", "", "path to key file")

	flag.Parse()

	err := webhook.Run(port, pathToCertFile, pathToKeyFile)
	if err != nil {
		klog.Fatal(err)
	}
}
