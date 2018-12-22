# Kubectl-debug

![license](https://img.shields.io/hexpm/l/plug.svg)
[![travis](https://travis-ci.org/aylei/kubectl-debug.svg?branch=master)](https://travis-ci.org/aylei/kubectl-debug)
[![Go Report Card](https://goreportcard.com/badge/github.com/aylei/kubectl-debug)](https://goreportcard.com/report/github.com/aylei/kubectl-debug)
[![docker](https://img.shields.io/docker/pulls/aylei/debug-agent.svg)](https://hub.docker.com/r/aylei/debug-agent)

[中文文档](./docs/zh-cn.md)

`kubectl-debug` is an out-of-tree solution for [troubleshooting running pods](https://github.com/kubernetes/community/blob/master/contributors/design-proposals/node/troubleshoot-running-pods.md), which allows you to run a new container in running pods for debugging purpose. The new container will join the `pid`, `network`, `user` and `ipc` namespaces of the target container, so you can use arbitrary trouble-shooting tools without pre-install them in your production container image.

> Compatibility: I've tested `kubectl-debug` with kubectl v1.13.1 and kubernetes v1.9.1. I don't have an environment to test more versions but I suppose that `kubectl-debug` is compatible with all versions of kubernetes and kubectl 1.12.0+. Please [file an issue](https://github.com/aylei/kubectl-debug/issues/new) if you find `kubectl-debug` do not work.

# Quick Start

Install the debug agent DaemonSet in your cluster, which is responsible to run the "new container":
```bash
kubectl apply -f https://raw.githubusercontent.com/aylei/kubectl-debug/master/scripts/agent_daemonset.yml
```

Install the kubectl debug plugin:
```bash
curl 
```

Try it out!
```bash
kubectl debug POD_NAME
# learn more with 
kubectl debug -h
```

# Default image and entrypoint

`kubectl-debug` use [nicolaka/netshoot](https://github.com/nicolaka/netshoot) as the default image to run debug container, and use `bash` as default entrypoint.

You can override the default image and entrypoint with cli flag, or even better, with config file `~/.kube/debug-config`:

```yaml
agent_port: 10027
image: nicolaka/netshoot:latest
command:
- '/bin/bash'
- '-l'
```

PS: `kubectl-debug` will always override the entrypoint of the container, which is by design to avoid users running an unwanted service by mistake(of course you can always do this explicitly).

# Details

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

# Contribute

Feel free to open issues and pull requests. Any feedback will be highly appreciated!