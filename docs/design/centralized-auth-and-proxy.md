# Centralized auth and proxy

## Problems

`kubectl-debug` is relying on the `debug-agent` DaemonSet to establish debug connection, which has the following problems:

* `debug-agent` runs in host network, but the node is not always accessible publicly, especially in the public cloud, e.g. GKE.
* `debug-agent` do not authz & authn request currently, which limits the usage of `kubectl debug` in serious environment.
* `debug-agent` is a rare operation, but `debug agent` is always consuming a few resources. We may consider agentless mode as an option.

## Proposal Design

The general idea is: instead of talking to `debug-agent` directly, `kubectl-debug` should talk to `apiserver` only.

To achieve this goal, we introduce an `ExtendAPIServer` to handle the debug request, our `ExtendAPIServer`(`server` in short) should do the following things:

* Transform the authz & authn request and delegate it to `APIServer`. In detail, `server` transform the `debug` request of a pod to a exec request of that pod, namely we inherit `debug` privilege from `exec`.
* Send request to debug launching pod.
  * debug launching pod is either a pod in pre-installed DaemonSet or a pod created on demand.
  * launching pod is responsible to create the debug container and proxy back the terminal.
* Proxy the terminal connection.
* Coordinate cleanups.

If we configure the `server` to create debug launching pod in target host on demand, then this is agent-less mode. Agent-less mode will leads to longer debug preparation time obviously.

There's still a security issue about the debug launching pod, because it has the privilege to operate the local containers directly, which is really a super user power.

Thus, we design the debug launching pod using an single purpose image: only the binary is installed like `distroless`.



