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
	LockName        string
	LockNamespace   string
	RetryPeriod     time.Duration
	LeaseDuration   time.Duration
	RenewDeadline   time.Duration
	Callback        func(leader string)
	RunningLockName string
	Done            <-chan struct{}
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

	callbacks := leaderelection.LeaderCallbacks{
		OnStartedLeading: func(ctx context.Context) {
			runExclusive(ctx, client, id, cfg)
		},
		OnStoppedLeading: func() {
			klog.Infof("Leader lost: %s", id)
		},
		OnNewLeader: func(identity string) {
			if identity == id {
				return
			}
			cfg.Callback(identity)
		},
	}

	leaderelection.RunOrDie(ctx, electionConfig(client, id, cfg.LockName, cfg, callbacks))
	klog.Info("Exiting election loop.")
}

func runExclusive(ctx context.Context, client *clientset.Clientset, id string, cfg Config) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	select {
	case <-cfg.Done:
		cfg.Callback(id)
		return
	default:
	}

	go func() {
		select {
		case <-cfg.Done:
			klog.Infof("Task done, releasing %s", cfg.RunningLockName)
			cancel()
		case <-ctx.Done():
		}
	}()

	callbacks := leaderelection.LeaderCallbacks{
		OnStartedLeading: func(ctx context.Context) {
			cfg.Callback(id)
		},
		OnStoppedLeading: func() {
			klog.Infof("Released %s", cfg.RunningLockName)
		},
		OnNewLeader: func(identity string) {
			if identity != id {
				klog.Infof("Waiting for %s to finish the task", identity)
			}
		},
	}

	leaderelection.RunOrDie(ctx, electionConfig(client, id, cfg.RunningLockName, cfg, callbacks))
}

func electionConfig(client *clientset.Clientset, id, lockName string, cfg Config, callbacks leaderelection.LeaderCallbacks) leaderelection.LeaderElectionConfig {
	// we use the Lease lock type since edits to Leases are less common
	// and fewer objects in the cluster watch "all Leases".
	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      lockName,
			Namespace: cfg.LockNamespace,
		},
		Client: client.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: id,
		},
	}

	return leaderelection.LeaderElectionConfig{
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
}
