package main

import (
	"context"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"
)

type Config struct {
	LockName      string
	LockNamespace string
	RetryPeriod   time.Duration
	LeaseDuration time.Duration
	RenewDeadline time.Duration
	Callback      func(leader string)
}

// Run creates and runs a new leader election
func Run(ctx context.Context, cfg Config) {
	id := os.Getenv("HOSTNAME")

	// We only care for inClusterConfig
	config, err := rest.InClusterConfig()
	if err != nil {
		klog.Fatalf("unable to get cluster config: %s", err.Error())
	}

	client := clientset.NewForConfigOrDie(config)

	// we use the Lease lock type since edits to Leases are less common
	// and fewer objects in the cluster watch "all Leases".
	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      cfg.LockName,
			Namespace: cfg.LockNamespace,
		},
		Client: client.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: id,
		},
	}

	callbacks := leaderelection.LeaderCallbacks{
		OnStartedLeading: func(ctx context.Context) {
			cfg.Callback(id)
		},
		OnStoppedLeading: func() {
			klog.Infof("Leader lost: %s", id)
		},
		OnNewLeader: func(identity string) {
			cfg.Callback(identity)
		},
	}

	leaderElectionConf := leaderelection.LeaderElectionConfig{
		Lock: lock,
		// IMPORTANT: you MUST ensure that any code you have that
		// is protected by the lease must terminate **before**
		// you call cancel. Otherwise, you could have a background
		// loop still running and another process could
		// get elected before your background loop finished, violating
		// the stated goal of the lease.
		ReleaseOnCancel: true,
		LeaseDuration:   cfg.LeaseDuration,
		RenewDeadline:   cfg.RenewDeadline,
		RetryPeriod:     cfg.RetryPeriod,
		Callbacks:       callbacks,
	}

	leaderelection.RunOrDie(ctx, leaderElectionConf)
	klog.Info("Exiting election loop.")
}
