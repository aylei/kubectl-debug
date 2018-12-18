# Kubectl-debug

`kubectl-debug` is an out-of-tree solution for [troubleshooting running pods](https://github.com/kubernetes/community/blob/master/contributors/design-proposals/node/troubleshoot-running-pods.md), which allows you to run new containers in running pod for debugging purpose.

Current status: proof of concept

# TODO

- [x] Skeleton
- [ ] Plugin: get HostIP and ContainerID(arbitrary one)
- [ ] Plugin: websockets client
- [ ] Agent: container manipulation
- [ ] Agent: websockets relay

nice to have:

- [ ] bash completion
- [ ] `kubectl debug list`: list debug containers, we might need this because the debug container is not discovered by kubernetes.
- [ ] security: security is import, but not a consideration in current stage

# Design

`kubectl-debug` consists of 2 components:

* the kubectl plugin: a cli client of `node agent`, serves `kubectl debug` command, 
* the node agent: responsible for manipulating the "debug container"; node agent will also act as a websockets relay for remote tty

General procedure:

When user run `kubectl debug target-pod -c <debug-container-name> /bin/bash`:

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

`kubectl exec` might be a good example.

# Reference

[sample-cli-plugin](https://github.com/kubernetes/sample-cli-plugin)

[docker api](https://godoc.org/github.com/docker/docker/client)

`kubectl exec` cli style:

```
Execute a command in a container.

Examples:
  # Get output from running 'date' from pod 123456-7890, using the first container by default
  kubectl exec 123456-7890 date

  # Get output from running 'date' in ruby-container from pod 123456-7890
  kubectl exec 123456-7890 -c ruby-container date

  # Switch to raw terminal mode, sends stdin to 'bash' in ruby-container from pod 123456-7890
  # and sends stdout/stderr from 'bash' back to the client
  kubectl exec 123456-7890 -c ruby-container -i -t -- bash -il

  # List contents of /usr from the first container of pod 123456-7890 and sort by modification time.
  # If the command you want to execute in the pod has any flags in common (e.g. -i),
  # you must use two dashes (--) to separate your command's flags/arguments.
  # Also note, do not surround your command and its flags/arguments with quotes
  # unless that is how you would execute it normally (i.e., do ls -t /usr, not "ls -t /usr").
  kubectl exec 123456-7890 -i -t -- ls -t /usr

Options:
  -c, --container='': Container name. If omitted, the first container in the pod will be chosen
  -i, --stdin=false: Pass stdin to the container
  -t, --tty=false: Stdin is a TTY

Usage:
  kubectl exec POD [-c CONTAINER] -- COMMAND [args...] [options]

Use "kubectl options" for a list of global command-line options (applies to all commands).
```


