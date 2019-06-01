# Kubectl-debug

![license](https://img.shields.io/hexpm/l/plug.svg)
[![travis](https://travis-ci.org/aylei/kubectl-debug.svg?branch=master)](https://travis-ci.org/aylei/kubectl-debug)
[![Go Report Card](https://goreportcard.com/badge/github.com/aylei/kubectl-debug)](https://goreportcard.com/report/github.com/aylei/kubectl-debug)
[![docker](https://img.shields.io/docker/pulls/aylei/debug-agent.svg)](https://hub.docker.com/r/aylei/debug-agent)

[简体中文](/docs/zh-cn.md)

# Overview

`kubectl-debug` is an out-of-tree solution for [troubleshooting running pods](https://github.com/kubernetes/community/blob/master/contributors/design-proposals/node/troubleshoot-running-pods.md), which allows you to run a new container in running pods for debugging purpose ([examples](/docs/examples.md)). The new container will join the `pid`, `network`, `user` and `ipc` namespaces of the target container, so you can use arbitrary trouble-shooting tools without pre-installing them in your production container image.

- [screenshots](#screenshots)
- [quick start](#quick-start)
- [build from source](#build-from-source)
- [port-forward and agentless](#port-forward-mode-And-agentless-mode)
- [configuration](#configuration)
- [roadmap](#roadmap)
- [authorization](#authorization)
- [contribute](#contribute)

# Screenshots

![gif](/docs/kube-debug.gif)

# Quick Start

## Install the kubectl debug plugin

Homebrew:
```shell
brew install aylei/tap/kubectl-debug
```

Download the binary:
```bash
export PLUGIN_VERSION=0.1.1
# linux x86_64
curl -Lo kubectl-debug.tar.gz https://github.com/aylei/kubectl-debug/releases/download/v${PLUGIN_VERSION}/kubectl-debug_${PLUGIN_VERSION}_linux_amd64.tar.gz
# macos
curl -Lo kubectl-debug.tar.gz https://github.com/aylei/kubectl-debug/releases/download/v${PLUGIN_VERSION}/kubectl-debug_${PLUGIN_VERSION}_darwin_amd64.tar.gz

tar -zxvf kubectl-debug.tar.gz kubectl-debug
sudo mv kubectl-debug /usr/local/bin/
```

For windows users, download the latest archive from the [release page](https://github.com/aylei/kubectl-debug/releases/tag/v0.1.1), decompress the package and add it to your PATH.

## (Optional) Install the debug agent DaemonSet

`kubectl-debug` requires an agent pod to communicate with the container runtime. In the [agentless mode](#port-forward-mode-And-agentless-mode), the agent pod can be created when a debug session starts and to be cleaned up when the session ends.

While convenient, creating pod before debugging can be time consuming. You can install the debug agent DaemonSet in advance to skip this:

```bash
kubectl apply -f https://raw.githubusercontent.com/aylei/kubectl-debug/master/scripts/agent_daemonset.yml
# or using helm
helm install -n=debug-agent ./contrib/helm/kubectl-debug
```

## Debug instructions

Try it out!

```bash
# kubectl 1.12.0 or higher
kubectl debug -h
# you can omit --agentless to reduce start time if you have installed the debug agent daemonset
# we will omit this flag in the following commands
kubectl debug POD_NAME --agentless

# in case of your pod stuck in `CrashLoopBackoff` state and cannot be connected to,
# you can fork a new pod and diagnose the problem in the forked pod
kubectl debug POD_NAME --fork

# if the node ip is not directly accessible, try port-forward mode
kubectl debug POD_NAME --port-forward --daemonset-ns=kube-system --daemonset-name=debug-agent

# old versions of kubectl cannot discover plugins, you may execute the binary directly
kubect-debug POD_NAME
```

* You can configure the default arguments to simplify usage, refer to [Configuration](#configuration)
* Refer to [Examples](/docs/examples.md) for practical debugging examples

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

# port-forward mode And agentless mode

- `port-foward` mode: By default, `kubectl-debug` will directly connect with the target host. When `kubectl-debug` cannot connect to `targetHost:agentPort`, you can enable `port-forward` mode. In `port-forward` mode, the local machine listens on `localhost:agentPort` and forwards data to/from `targetPod:agentPort`.

- `agentless` mode: By default, `debug-agent` needs to be pre-deployed on each node of the cluster, which consumes cluster resources all the time. Unfortunately, debugging Pod is a low-frequency operation. To avoid loss of cluster resources, the `agentless` mode has been added in [#31](https://github.com/aylei/kubectl-debug/pull/31). In `agentless` mode, `kubectl-debug` will first start `debug-agent` on the host where the target Pod is located, and then `debug-agent`  starts the debug container. After the user exits, `kubectl-debug` will delete the debug container and `kubectl-debug` will delete the `debug-agent` pod  at last.

# Configuration

`kubectl-debug` uses [nicolaka/netshoot](https://github.com/nicolaka/netshoot) as the default image to run debug container, and use `bash` as default entrypoint.

You can override the default image and entrypoint with cli flag, or even better, with config file `~/.kube/debug-config`:

```yaml
# debug agent listening port(outside container)
# default to 10027
agentPort: 10027

# whether using agentless mode
# default to false
agentless: true
# namespace of debug-agent pod, used in agentless mode
# default to 'default'
agentPodNamespace: default
# prefix of debug-agent pod, used in agentless mode
# default to  'debug-agent-pod'
agentPodNamePrefix: debug-agent-pod
# image of debug-agent pod, used in agentless mode
# default to 'aylei/debug-agent:latest'
agentImage: aylei/debug-agent:latest

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

# Authorization

Currently, `kubectl-debug` reuse the privilege of the `pod/exec` sub resource to do authorization, which means that it has the same privilege requirements with the `kubectl exec` command.

# Roadmap

`kubectl-debug` is supposed to be just a troubleshooting helper, and is going be replaced by the native `kubectl debug` command when [this proposal](https://github.com/kubernetes/community/blob/master/contributors/design-proposals/node/troubleshoot-running-pods.md) is implemented and merged in the future kubernetes release. But for now, there is still some works to do to improve `kubectl-debug`.

- [ ] Security: currently, `kubectl-debug` do authorization in the client-side, which should be moved to the server-side (debug-agent)
- [ ] More unit tests
- [ ] More real world debugging example
- [ ] e2e tests

If you are interested in any of the above features, please file an issue to avoid potential duplication.

# Contribute

Feel free to open issues and pull requests. Any feedback is highly appreciated!

# Acknowledgement

This project would not be here without the effort of [our contributors](https://github.com/aylei/kubectl-debug/graphs/contributors), thanks!
