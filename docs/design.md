# Under the hood

`kubectl-debug` consists of 2 components:

* the kubectl plugin: a cli client of `node agent`, serves `kubectl debug` command, 
* the node agent: responsible for manipulating the "debug container"; node agent will also act as a websockets relay for remote tty

When user run `kubectl debug target-pod -c <container-name> /bin/bash`:

1. The plugin gets the pod info from apiserver and extract the `hostIP`, if the target container does not exist or is not currently running, an error is raised.
2. The plugin sends an HTTP request to the specific node agent running on the `hostIP`, which includes a protocol upgrade from HTTP to SPDY.
3. The agent runs a container in the pod's namespaces (ipc, pid, network, etc) with the STDIN stay open (`-i` flag).
4. The agent checks if the target container is actively running, if not, write an error to client.
5. The agent runs a `debug container` with `tty` and `stdin` opened, the `debug container` will join the `pid`, `network`, `ipc` and `user` namespace of the target container.
6. The agent pipes the connection into the `debug container` using `attach`
7. **Debug in the debug container**.
8. Job is done, user closes the SPDY connection.
9. The node agent closes the SPDY connection, then waits for the `debug container` to exit and do the cleanup.