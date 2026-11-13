# Leader Elector

A simple leader election sidecar container for Kubernetes based on the [client-go](https://github.com/kubernetes/kubernetes/blob/master/staging/src/k8s.io/client-go/examples/leader-election/main.go) library.

Each replica of a Deployment runs the elector as a sidecar. The replicas compete for a `Lease` (`coordination.k8s.io`), and every pod can ask its own sidecar who the leader is over HTTP.

## Running the sidecar

Run the elector as a [native sidecar](https://kubernetes.io/docs/concepts/workloads/pods/sidecar-containers/) (an `initContainer` with `restartPolicy: Always`, Kubernetes 1.29+). It then starts before the rest of the pod's init containers and keeps running for the pod's lifetime. A plain `initContainer` won't work: the elector never exits, so the containers after it would never start.

The pod's service account needs `get`, `create` and `update` on `leases.coordination.k8s.io` in its namespace. See [example/rbac.yaml](./example/rbac.yaml).

The elector identifies each pod by `$HOSTNAME`, which is the pod name unless you override `hostname` in the pod spec.

## HTTP API

| Method | Path | Response |
|---|---|---|
| `GET` | `/` | `{"name": "<pod name>"}`: the leader as seen by this pod. `name` is `""` while it isn't known yet. |
| `POST` | `/done` | Tells the elector that this pod's task has finished. Only has an effect in [revision mode](#revision-mode-one-shot-tasks-per-rollout). |

A pod is the leader when `name` equals its own pod name (pass it in with the downward API, `metadata.name`).

## Choosing a mode

| | Simple mode | Revision mode |
|---|---|---|
| Enabled by | default | `--revision` / `ELECTION_REVISION` |
| Lease(s) | `<election>` | `<election>-<revision>` and `<election>-running` |
| Leader | one leader across all pods, including old and new revisions during a rollout | one leader per revision (rollout) |
| Good for | long-running singleton work in the main container | one-shot work that must run once per rollout, such as database migrations |
| Caller must POST `/done` | no | yes |

### Simple mode: long-running singleton work

Every pod sees the same leader. During a rollout an old pod may stay leader until it terminates, after which a new pod takes over.

Leadership can move at any time (the leader is deleted or evicted, or loses connection to the API server), so poll `/` and only do leader work while `name` is your own pod name:

```sh
while true; do
  NAME=$(curl -s http://localhost:4040 | jq -r '.name')
  if [ "$NAME" = "$POD_NAME" ]; then
    do_leader_work
  fi
  sleep 5
done
```

See [example/deployment.yaml](./example/deployment.yaml).

### Revision mode: one-shot tasks per rollout

Set `--revision` to the pod's `pod-template-hash` label, which is different for every rollout of a Deployment:

```yaml
env:
  - name: ELECTION_REVISION
    valueFrom:
      fieldRef:
        fieldPath: metadata.labels['pod-template-hash']
```

How it works:

1. The pods of each revision elect their own leader on the `<election>-<revision>` lease, so a new rollout always gets a leader of its own instead of keeping the old ReplicaSet's leader.
2. Before reporting itself as leader, the winning pod also takes the `<election>-running` lease, which every revision shares. This makes a new revision's leader wait until the previous revision's leader is done, so the task never runs twice at the same time.
3. The leader releases `<election>-running` when it receives `POST /done`, or when the pod shuts down.

What the task container needs to do:

1. **Poll `/` until `name` isn't empty.** While the leader waits for `<election>-running`, its own sidecar returns `""`. A single check right after start-up would mistake that for "not the leader" and skip the task.
2. If `name` is your own pod name, run the task.
3. **Always `POST /done` when you're finished, on every pod, leader or not.**
   - On the leader, this releases `<election>-running` so the next rollout's leader can start. If you don't send it, the lease stays held until the pod terminates. If the new pods can't become Ready until their task runs, the rollout gets stuck: the new leader waits for the old pod to go away, and the old pod isn't removed until the new pods are Ready.
   - On other pods, it means that if one of them later takes over as revision leader, it won't take `<election>-running` and then hold it with no task container left to release it.
   - Only send it after the task **succeeds**. If the task fails, exit non-zero without calling `/done`. Kubernetes restarts the init container, which sees itself as leader again and retries the task.

```sh
set -e
until NAME=$(curl -sf http://localhost:4040 | jq -r '.name') && [ -n "$NAME" ]; do
  sleep 1
done

if [ "$NAME" = "$POD_NAME" ]; then
  run_migrations
fi

curl -sf -X POST http://localhost:4040/done
```

See [example/deployment-revision.yaml](./example/deployment-revision.yaml).

Notes:

- `--revision` can't be `running`, because that name is used for the shared lease.
- Each rollout leaves its `<election>-<revision>` Lease behind. They're harmless, but you can delete old ones with `kubectl delete lease`.
- Switching an existing Deployment to revision mode is safe. Pods from the old revision run in simple mode, never take `<election>-running`, and so don't block the first rollout.

## Configuration

| Flag | Env var | Default | Purpose |
|---|---|---|---|
| `--election` | `ELECTION_NAME` | `default` | Name of the election, used as the Lease name (or its prefix) |
| `--namespace` | `ELECTION_NAMESPACE` | `default` | Namespace of the Lease(s) |
| `--revision` | `ELECTION_REVISION` | (unset) | Enables revision mode; set to the `pod-template-hash` label |
| `--lease-duration` | `ELECTION_LEASE_DURATION` | `15s` | How long other pods wait before taking over a lease that hasn't been renewed |
| `--renew-deadline` | `ELECTION_RENEW_DEADLINE` | `10s` | How long the leader keeps retrying to renew before giving up |
| `--retry-period` | `ELECTION_RETRY_PERIOD` | `2s` | Delay between election attempts |
| `--port` | `ELECTION_PORT` | `4040` | Port of the HTTP API |
