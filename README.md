# Kubectl-debug

`kubectl-debug` is an out-of-tree solution for [troubleshooting running pods](https://github.com/kubernetes/community/blob/master/contributors/design-proposals/node/troubleshoot-running-pods.md), which allows you to run new containers in running pod for debugging purpose.

Current status: proof of concept

# Design

`kubectl-debug` consists of 2 components:

* the kubectl plugin: a cli client of `node agent`, serves `kubectl plugin debug` command, 
* the node agent: responsible for manipulating the "debug container"; node agent will also act as a websockets relay for remote tty

General procedure:

When user run `kubectl plugin debug target-pod -c <debug-container-name> /bin/bash`:

1. The plugin get the pod info from apiserver and extract the `hostIP`, if the pod is no existed or not currently running, an error raised.
2. The plugin send a HTTP request to the specific node agent running on the `hostIP`, which includes a protocol upgrade from HTTP to WebSockets.
3. The node agent runs a container in the pod's namespaces (ipc, pid, network, etc) with the STDIN stay open (`-i` flag).
4. The node agent binds the STDIN/STDOUT/STDERR of container, serves those streams to client by WebSockets.
5. **Debug in the debug container**.
6. Jobs done, user close the Websockets connection.
7. The node agent close the Websockets connection, then cleanup the debug container.

Cli consideration:

* We should provide `--image` flag for user to override the default image for debug container.
* User may omit `-c` flag, the node agent can just pick one for user with certain prefix to indicates that it is a debug container.
* We can provide a flag to retain the debug container after connection closed.

Additional consideration:

* What will happen if the pod exits when the debug container running in it.
* What will happen if the debug container get killed or stuck.
