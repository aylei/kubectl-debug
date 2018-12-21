# Kubectl-debug

> Pod debugging made easy

`kubectl-debug` is an out-of-tree solution for [troubleshooting running pods](https://github.com/kubernetes/community/blob/master/contributors/design-proposals/node/troubleshoot-running-pods.md), which allows you to run new containers in running pod for debugging purpose. `kubectl-debug` is pretty simple and capable for all versions* of k8s.

`*`: I've tested `kubectl-debug` with kubectl version v1.13.1 and kubernetes version v1.9.1. I don't have an environment to test more versions but I suppose that `kubectl-debug` is compatible with all versions of kubernetes and kubectl 1.12.0 or higher. Please [file an issue] if you find `kubectl-debug` do not work.

# Quick Start

WIP

# TODO

- [ ] DaemonSet YAML and helm chart for agent

nice to have:

- [ ] bash completion
- [ ] `kubectl debug list`: list debug containers, we might need this because the debug container is not discovered by kubernetes.
- [ ] security: security is import, but not a consideration in current stage

# Design

`kubectl-debug` consists of 2 components:

* the kubectl plugin: a cli client of `node agent`, serves `kubectl debug` command, 
* the node agent: responsible for manipulating the "debug container"; node agent will also act as a websockets relay for remote tty

When user run `kubectl debug target-pod -c <container-name> /bin/bash`:

1. The plugin get the pod info from apiserver and extract the `hostIP`, if the target container is no existed or not currently running, an error raised.
2. The plugin send a HTTP request to the specific node agent running on the `hostIP`, which includes a protocol upgrade from HTTP to SPDY.
3. The agent runs a container in the pod's namespaces (ipc, pid, network, etc) with the STDIN stay open (`-i` flag).
4. The agent checks if the target container is actively running, if not, write an error to client.
5. The agent runs a `debug container` with `tty` and `stdin` opened, the `debug contaienr` will join the `pid`, `network`, `ipc` and `user` namespace of the target container.
6. The agent pipes the connection io to the `debug contaienr` using `attach`
7. **Debug in the debug container**.
8. Jobs done, user close the SPDY connection.
9. The node agent close the SPDY connection, then wait the `debug contaienr` exit and do the cleanup.

# Reference

[sample-cli-plugin](https://github.com/kubernetes/sample-cli-plugin)

[docker api](https://godoc.org/github.com/docker/docker/client)
