package main

import (
	"k8s.io/klog/v2"
)

func handleError(log string, err error) {
	if err != nil {
		klog.Exit(log, err)
	}
}
