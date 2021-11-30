# Under the hood

`kubectl-debug` consists of 3 components:

* the 'kubectl-debug' executable serves the `kubectl-debug` command and interfaces with the kube-api-server
* the 'debug-agent' pod is a temporary pod that is started in the cluster by kubectl-debug. The 'debug-agent' container is responsible for starting and manipulating the 'debug container'. The 'debug-agent' will also act as a websockets relay for remote tty to join the output of the 'debug container' to the terminal from which the kubectl-debug command was issued
* the 'debug container' which is the container that provides the debugging utilities and the shell in which the human user performs their debugging activity. `kubectl-debug` doesn't provide this - it's an 'off-the-shelf container image (nicolaka/netshoot:latest by default), it is invoked and configured by 'debug-agent'.

When user runs `kubectl-debug --namespace <namespace> <target-pod> -c <container-name>`

1. 'kubectl-debug' gets the target-pod info from kube-api-server and extracts the `host` information (the target-pod within the namespace <namespace>)
2. 'kubectl-debug' sends a 'debug-agent' pod specification to kube-api-server with a node-selector matching the `host`
3. kube-api-server requests the creation of 'debug-agent' pod. 'debug-agent' pod is created in the default namespace (doesn't have to be the same namespace as the target pod)
4. 'kubectl-debug' sends an HTTP request to the 'debug-agent' pod running on the `host` which includes a protocol upgrade from HTTP to SPDY
5. debug-agent' checks if the target container is actively running, if not, write an error to client
6. 'debug-agent' interfaces with containerd (or dockerd if applicable) on the host to request the creation of the 'debug-container'. `debug container` with `tty` and `stdin` opened, the 'debug-agent' configures the `debug container` `pid`, `network`, `ipc` and `user` namespace to be that of the target container
7. 'debug-agent' pipes the connection into the `debug container` using `attach`
8. Human performs debugging/troubleshooting on the target container from 'within' in the debug container with access to the target container process/network/ipc namespaces and root filesystem
9. debugging complete, user exits the debug-container shell which closes the SPDY connection
10. 'debug-agent' closes the SPDY connection, then waits for the `debug container` to exit and do the cleanup
11. 'debug-agent' pod is deleted