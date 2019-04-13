# Kubectl-debug

![license](https://img.shields.io/hexpm/l/plug.svg)
[![travis](https://travis-ci.org/aylei/kubectl-debug.svg?branch=master)](https://travis-ci.org/aylei/kubectl-debug)
[![Go Report Card](https://goreportcard.com/badge/github.com/aylei/kubectl-debug)](https://goreportcard.com/report/github.com/aylei/kubectl-debug)
[![docker](https://img.shields.io/docker/pulls/aylei/debug-agent.svg)](https://hub.docker.com/r/aylei/debug-agent)

[中文](./docs/zh-cn.md)

# Overview

`kubectl-debug` is an out-of-tree solution for [troubleshooting running pods](https://github.com/kubernetes/community/blob/master/contributors/design-proposals/node/troubleshoot-running-pods.md), which allows you to run a new container in running pods for debugging purpose. The new container will join the `pid`, `network`, `user` and `ipc` namespaces of the target container, so you can use arbitrary trouble-shooting tools without pre-installing them in your production container image.

- [demo](#demo)
- [quick start](#quick-start)
- [build from source](#build-from-source)
- [default image and entrypoints](#default-image-and-entrypoint)
- [future works](#future-works)
- [implementation details](#details)
- [contribute](#contribute)

# Screenshots

![gif](./docs/kube-debug.gif)

# Quick Start

`kubectl-debug` is pretty simple, give it a try!

Install the debug agent DaemonSet in your cluster, which is responsible for running the "debug container":

```bash
kubectl apply -f https://raw.githubusercontent.com/aylei/kubectl-debug/master/scripts/agent_daemonset.yml
# or using helm
helm install -n=debug-agent ./contrib/helm/kubectl-debug
```

Install the kubectl debug plugin:

Using [krew]():
```shell
```

Homebrew:
```shell
brew install aylei/tap/kubectl-debug
```

Download the binary:
```bash
export PLUGIN_VERSION=0.1.0
# linux x86_64
curl -Lo kubectl-debug https://github.com/aylei/kubectl-debug/releases/download/${PLUGIN_VERSION}/kubectl-debug_${PLUGIN_VERSION}_linux-amd64
# macos
curl -Lo kubectl-debug https://github.com/aylei/kubectl-debug/releases/download/${PLUGIN_VERSION}/kubectl-debug_${PLUGIN_VERSION}_darwin-amd64

chmod +x ./kubectl-debug
sudo mv kubectl-debug /usr/local/bin/
```

For windows users, download the latest archive from the [release page](https://github.com/aylei/kubectl-debug/releases/tag/0.0.2) and decompress it to your PATH.

Try it out!
```bash
# kubectl 1.12.0 or higher
kubectl debug -h
kubectl debug POD_NAME

# in case of your pod stuck in `CrashLoopBackoff` state and cannot be connected to,
# you can fork a new pod and diagnose the problem in the forked pod
kubectl debug POD_NAME --fork

# if the node ip is not directly accessible, try port-forward mode
kubectl debug POD_NAME --port-fowrad --daemonset-ns=kube-system --daemonset-name=debug-agent

# old versions of kubectl cannot discover plugins, you may execute the binary directly
kubect-debug POD_NAME

```

Any trouble? [file and issue for help](https://github.com/aylei/kubectl-debug/issues/new)

# Build from source

Clone this repo and:
```bash
# make will build plugin binary and debug-agent image
make
# install plugin
mv kubectl-debug /usr/local/bin

# build plugin only
make plugin
# build agent only
make agent-docker
```

# Configurations

`kubectl-debug` uses [nicolaka/netshoot](https://github.com/nicolaka/netshoot) as the default image to run debug container, and use `bash` as default entrypoint.

You can override the default image and entrypoint with cli flag, or even better, with config file `~/.kube/debug-config`:

```yaml
# debug agent listening port
# default to 10027
agentPort: 10027
# daemonset name of the debug-agent, used in port-forward
# default to 'debug-agent'
debugAgentDaemonset: debug-agent
# daemonset namespace of the debug-agent, used in port-forwad
# default to 'default'
debugAgentNamespace: kube-system
# whether using port-forward when connecting debug-agent
# default false
portForward: true
# image of the debug container
# default as showed
image: nicolaka/netshoot:latest
# start command of the debug container
# default ['bash']
command:
- '/bin/bash'
- '-l'
```

If the debug-agent is not accessible from host port, it is recommended to set `portForward: true` to using port-forawrd mode.

PS: `kubectl-debug` will always override the entrypoint of the container, which is by design to avoid users running an unwanted service by mistake(of course you can always do this explicitly).

# Roadmap

`kubectl-debug` is supposed to be just a troubleshooting helper, and is going be replaced by the native `kubectl debug` command when [this proposal](https://github.com/kubernetes/community/blob/master/contributors/design-proposals/node/troubleshoot-running-pods.md) is implemented and merged in the future kubernetes release. But for now, there is still some works to do to improve `kubectl-debug`.

If you are interested in any of following features, please file an issue to avoid potential duplication.

- [ ] Security. `kubectl-debug` runs privileged agent on every node, and client talks to the agent directly. A possible solution is introducing a central apiserver to do RBAC, which integrates to the kube apiserver using [aggregation layer](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/apiserver-aggregation/)
- [ ] Protocol. `kubectl-debug` vendor the SPDY wrapper from `client-go`. SPDY is deprecated now, websockets may be a better choice
- [ ] e2e tests.

# Details

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

# Contribute

Feel free to open issues and pull requests. Any feedback is highly appreciated!
