package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/alexflint/go-arg"
	"k8s.io/klog/v2"
)

var (
	args struct {
		LockName      string        `arg:"--election,env:ELECTION_NAME" default:"default" help:"Name of this election"`
		Namespace     string        `arg:"env:ELECTION_NAMESPACE" default:"default" help:"Namespace of this election"`
		RenewDeadline time.Duration `arg:"--renew-deadline,env:ELECTION_RENEW_DEADLINE" default:"10s" help:"Duration that the acting leader will retry refreshing leadership before giving up"`
		RetryPeriod   time.Duration `arg:"--retry-period,env:ELECTION_RETRY_PERIOD" default:"2s" help:"Duration between each action retry"`
		LeaseDuration time.Duration `arg:"--lease-duration,env:ELECTION_LEASE_DURATION" default:"15s" help:"Duration that non-leader candidates will wait after observing a leadership renewal until attempting to acquire leadership of a led but unrenewed leader slot"`
		Port          string        `arg:"env:ELECTION_PORT" default:"4040" help:"Port on which to query the leader"`
		Revision      string        `arg:"--revision,env:ELECTION_REVISION" help:"Revision to scope this election to, typically the pod-template-hash label, so each rollout elects its own leader"`
	}
	leader Leader

	taskDone      = make(chan struct{})
	closeTaskDone sync.Once
)

// Leader contains the name of the current leader of this election
type Leader struct {
	Name string `json:"name"`
}

func leaderHandler(res http.ResponseWriter, req *http.Request) {
	data, err := json.Marshal(leader)
	if err != nil {
		klog.Errorf("Error while marshaling leader response: %s", err.Error())
		res.WriteHeader(http.StatusInternalServerError)
		return
	}
	res.Write(data)
}

func doneHandler(res http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		res.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	closeTaskDone.Do(func() { close(taskDone) })
}

func main() {
	parser := arg.MustParse(&args)
	if args.Revision == "running" {
		parser.Fail(`--revision cannot be "running", it would collide with the <election>-running lease`)
	}

	// use a Go context so we can tell the leaderelection code when we
	// want to step down
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// listen for interrupts or the Linux SIGTERM signal and cancel
	// our context, which the leader election code will observe and
	// step down
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		klog.Info("Received termination, signaling shutdown")
		cancel()
	}()

	// configuring HTTP server
	http.HandleFunc("/", leaderHandler)
	http.HandleFunc("/done", doneHandler)
	server := &http.Server{Addr: ":" + args.Port, Handler: nil}
	go func() {
		if err := server.ListenAndServe(); err != nil {
			klog.Fatal(err)
		}
	}()

	// configuring Leader Election loop
	callback := func(name string) {
		klog.Infof("Currently leading: %s", name)
		leader = Leader{name}
	}

	lockName := args.LockName
	if args.Revision != "" {
		lockName += "-" + args.Revision
	}

	electionConfig := Config{
		LockName:      lockName,
		LockNamespace: args.Namespace,
		RenewDeadline: args.RenewDeadline,
		RetryPeriod:   args.RetryPeriod,
		LeaseDuration: args.LeaseDuration,
		Callback:      callback,

		RunningLockName: args.LockName + "-running",
		Done:            taskDone,
	}

	Run(ctx, electionConfig)

	// gracefully stop HTTP server
	srvCtx, srvCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer srvCancel()
	if err := server.Shutdown(srvCtx); err != nil {
		klog.Fatal(err)
	}
}
