# Kubectl-debug

![license](https://img.shields.io/hexpm/l/plug.svg)
[![travis](https://travis-ci.org/aylei/kubectl-debug.svg?branch=master)](https://travis-ci.org/aylei/kubectl-debug)
[![Go Report Card](https://goreportcard.com/badge/github.com/aylei/kubectl-debug)](https://goreportcard.com/report/github.com/aylei/kubectl-debug)
[![docker](https://img.shields.io/docker/pulls/aylei/debug-agent.svg)](https://hub.docker.com/r/aylei/debug-agent)

[简体中文](/docs/zh-cn.md)

# Overview

`kubectl-debug` is an out-of-tree solution for [troubleshooting running pods](https://github.com/kubernetes/community/blob/master/contributors/design-proposals/node/troubleshoot-running-pods.md), which allows you to run a new container in running pods for debugging purpose ([examples](/docs/examples.md)). The new container will join the `pid`, `network`, `user` and `ipc` namespaces of the target container, so you can use arbitrary trouble-shooting tools without pre-installing them in your production container image.

- [Kubectl-debug](#kubectl-debug)
- [Overview](#overview)
- [Screenshots](#screenshots)
- [Quick Start](#quick-start)
  - [Install the kubectl debug plugin](#install-the-kubectl-debug-plugin)
  - [(Optional) Install the debug agent DaemonSet](#optional-install-the-debug-agent-daemonset)
  - [Debug instructions](#debug-instructions)
- [Build from source](#build-from-source)
- [port-forward mode And agentless mode(Default opening)](#port-forward-mode-and-agentless-modedefault-opening)
- [Configuration](#configuration)
- [Authorization](#authorization)
- [Roadmap](#roadmap)
- [Contribute](#contribute)
- [Acknowledgement](#acknowledgement)

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

`kubectl-debug` requires an agent pod to communicate with the container runtime. In the [agentless mode](#port-forward-mode-And-agentless-mode), the agent pod can be created when a debug session starts and to be cleaned up when the session ends.(Turn on agentless mode by default)

While convenient, creating pod before debugging can be time consuming. You can install the debug agent DaemonSet and use --agentless=false params in advance to skip this:

```bash
# if your kubernetes version is v1.16 or newer
kubectl apply -f https://raw.githubusercontent.com/aylei/kubectl-debug/master/scripts/agent_daemonset.yml
# if your kubernetes is old version(<v1.16), you should change the apiVersion to extensions/v1beta1, As follows
wget https://raw.githubusercontent.com/aylei/kubectl-debug/master/scripts/agent_daemonset.yml
sed -i '' '1s/apps\/v1/extensions\/v1beta1/g' agent_daemonset.yml
kubectl apply -f agent_daemonset.yml
# or using helm
helm install kubectl-debug -n=debug-agent ./contrib/helm/kubectl-debug
# use daemonset agent mode(close agentless mode)
kubectl debug --agentless=false POD_NAME
```

## Debug instructions

Try it out!

```bash
# kubectl 1.12.0 or higher
kubectl debug -h
# if you installed the debug agent's daemonset, you can use --agentless=false to speed up the startup.
# the default agentless mode will be used in following commands
kubectl debug POD_NAME

# in case of your pod stuck in `CrashLoopBackoff` state and cannot be connected to,
# you can fork a new pod and diagnose the problem in the forked pod
kubectl debug POD_NAME --fork

# in fork mode, if you want the copied pod retains the labels of the original pod, you can use the --fork-pod-retain-labels parameter to set(comma separated, and spaces are not allowed)
# Example is as follows
# If not set, this parameter is empty by default (Means that any labels of the original pod are not retained, and the labels of the copied pods are empty.)
kubectl debug POD_NAME --fork --fork-pod-retain-labels=<labelKeyA>,<labelKeyB>,<labelKeyC>

# in order to enable node without public IP or direct access (firewall and other reasons) to access, port-forward mode is enabled by default.
# if you don't need to turn on port-forward mode, you can use --port-forward false to turn off it.
kubectl debug POD_NAME --port-forward=false --agentless=false --daemonset-ns=kube-system --daemonset-name=debug-agent

# old versions of kubectl cannot discover plugins, you may execute the binary directly
kubectl-debug POD_NAME

# use primary docker registry, set registry kubernets secret to pull image
# the default registry-secret-name is kubectl-debug-registry-secret, the default namespace is default
# please set the secret data source as {Username: <username>, Password: <password>}
kubectl-debug POD_NAME --image calmkart/netshoot:latest --registry-secret-name <k8s_secret_name> --registry-secret-namespace <namespace>
# in default agentless mode, you can set the agent pod's resource limits/requests, for example:
# default is not set
kubectl-debug POD_NAME --agent-pod-cpu-requests=250m --agent-pod-cpu-limits=500m --agent-pod-memory-requests=200Mi --agent-pod-memory-limits=500Mi
```

Example:
```bash
# how to create a private docker registry secret
# take the user name 'calmkart' password 'calmkart' as an example
# refer to the official kubernetes documentation for more ways to create
# https://kubernetes.io/docs/concepts/configuration/secret/
echo -n '{Username: calmkart, Password: calmkart}' > ./registrySecret.txt
kubectl create secret generic kubectl-debug-registry-secret --from-file=./registrySecret.txt
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

# port-forward mode And agentless mode(Default opening)

- `port-foward` mode: By default, `kubectl-debug` will directly connect with the target host. When `kubectl-debug` cannot connect to `targetHost:agentPort`, you can enable `port-forward` mode. In `port-forward` mode, the local machine listens on `localhost:agentPort` and forwards data to/from `targetPod:agentPort`.

- `agentless` mode: By default, `debug-agent` needs to be pre-deployed on each node of the cluster, which consumes cluster resources all the time. Unfortunately, debugging Pod is a low-frequency operation. To avoid loss of cluster resources, the `agentless` mode has been added in [#31](https://github.com/aylei/kubectl-debug/pull/31). In `agentless` mode, `kubectl-debug` will first start `debug-agent` on the host where the target Pod is located, and then `debug-agent`  starts the debug container. After the user exits, `kubectl-debug` will delete the debug container and `kubectl-debug` will delete the `debug-agent` pod at last.

# Configuration

`kubectl-debug` uses [nicolaka/netshoot](https://github.com/nicolaka/netshoot) as the default image to run debug container, and use `bash` as default entrypoint.

You can override the default image and entrypoint with cli flag, or even better, with config file `~/.kube/debug-config`:

```yaml
# debug agent listening port(outside container)
# default to 10027
agentPort: 10027

# whether using agentless mode
# default to true
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
# default true
portForward: true
# image of the debug container
# default as showed
image: nicolaka/netshoot:latest
# start command of the debug container
# default ['bash']
command:
- '/bin/bash'
- '-l'
# private docker registry auth kuberntes secret
# default RegistrySecretName is kubectl-debug-registry-secret
# default namspace is default
RegistrySecretName: my-debug-secret
RegistrySecretNamespace: debug
# in agentless mode, you can set the agent pod's resource limits/requests:
# default is not set
agentCpuRequests: ""
agentCpuLimits: ""
agentMemoryRequests: ""
agentMemoryLimits: ""
# in fork mode, if you want the copied pod retains the labels of the original pod, you can change this params
# format is []string
# If not set, this parameter is empty by default (Means that any labels of the original pod are not retained, and the labels of the copied pods are empty.)
forkPodRetainLabels: []
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
