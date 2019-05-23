# Kubectl debug

`kubectl-debug` 是一个简单的 kubectl 插件, 能够帮助你便捷地进行 Kubernetes 上的 Pod 排障诊断. 背后做的事情很简单: 在运行中的 Pod 上额外起一个新容器, 并将新容器加入到目标容器的 `pid`, `network`, `user` 以及 `ipc` namespace 中, 这时我们就可以在新容器中直接用 `netstat`, `tcpdump` 这些熟悉的工具来解决问题了, 而旧容器可以保持最小化, 不需要预装任何额外的排障工具.

# Quick Start

`kubectl-debug` 包含两部分, 一部分是用户侧的 kubectl 插件, 另一部分是部署在所有 k8s 节点上的 agent(用于启动"新容器", 同时也作为 SPDY 连接的中继). 因此首先要部署 agent.
```bash
kubectl apply -f https://raw.githubusercontent.com/aylei/kubectl-debug/master/scripts/agent_daemonset.yml
# 或者使用 helm 安装
helm install -n=debug-agent ./contrib/helm/kubectl-debug
```

安装 kubectl 插件:

使用 [krew](https://github.com/kubernetes-sigs/krew):
```shell
# Waiting the krew index PR to be merged...
```

使用 Homebrew:
```shell
brew install aylei/tap/kubectl-debug
```

直接下载预编译的压缩包:
```bash
export PLUGIN_VERSION=0.1.0
# linux x86_64
curl -Lo kubectl-debug.tar.gz https://github.com/aylei/kubectl-debug/releases/download/v${PLUGIN_VERSION}/kubectl-debug_${PLUGIN_VERSION}_linux_amd64.tar.gz
# macos
curl -Lo kubectl-debug.tar.gz https://github.com/aylei/kubectl-debug/releases/download/v${PLUGIN_VERSION}/kubectl-debug_${PLUGIN_VERSION}_darwin_amd64.tar.gz

tar -zxvf kubectl-debug.tar.gz kubectl-debug
sudo mv kubectl-debug /usr/local/bin/
```

Windows 用户可以从 [release page](https://github.com/aylei/kubectl-debug/releases/tag/v0.1.0) 进行下载并添加到 PATH 中

简单使用:
```bash
# kubectl 1.12.0 or higher
kubectl debug -h
kubectl debug POD_NAME

# in case of your pod stuck in `CrashLoopBackoff` state and cannot be connected to,
# you can fork a new pod and diagnose the problem in the forked pod
kubectl debug POD_NAME --fork

# if the node ip is not directly accessible, try port-forward mode
kubectl debug POD_NAME --port-forward --daemonset-ns=kube-system --daemonset-name=debug-agent

# old versions of kubectl cannot discover plugins, you may execute the binary directly
kubect-debug POD_NAME

```

Any trouble? [file and issue for help](https://github.com/aylei/kubectl-debug/issues/new)


# port-forward 模式和 agentless 模式

- `port-foward`模式：默认情况下，`kubectl-debug`会直接与目标宿主机建立连接。当`kubectl-debug`无法与目标宿主机直连时，可以开启`port-forward`模式。`port-forward`模式下，本机会监听localhost:agentPort，并将数据转发至目标Pod的agentPort端口。

- `agentless`模式： 默认情况下，`debug-agent`需要预先部署在集群每个节点上，会一直消耗集群资源，然而调试 Pod 是低频操作。为避免集群资源损失，在[#31](https://github.com/aylei/kubectl-debug/pull/31)增加了`agentless`模式。`agentless`模式下，`kubectl-debug`会先在目标Pod所在宿主机上启动`debug-agent`，然后再启动调试容器。用户调试结束后，`kubectl-debug`会依次删除调试容器和在目的主机启动的`degbug-agent`。


# 默认镜像和 Entrypoint

`kubectl-debug` 使用 [nicolaka/netshoot](https://github.com/nicolaka/netshoot) 作为默认镜像. 默认镜像和指令都可以通过命令行参数进行覆盖. 考虑到每次都指定有点麻烦, 也可以通过文件配置的形式进行覆盖, 编辑 `~/.kube/debug-config` 文件:

```yaml
# debug-agent 映射到宿主机的端口
# 默认 10027
agentPort: 10027

# 是否开启ageless模式
# 默认 false
agentless: true
# agentPod 的 namespace, agentless模式可用
# 默认 default
agentPodNamespace: default
# agentPod 的名称前缀，后缀是目的主机名, agentless模式可用
# 默认 debug-agent-pod
agentPodNamePrefix: debug-agent-pod
# agentPod 的镜像, agentless模式可用
# 默认 aylei/debug-agent:latest
agentImage: aylei/debug-agent:latest

# debug-agent DaemonSet 的名字, port-forward 模式时会用到
# 默认 'debug-agent'
debugAgentDaemonset: debug-agent
# debug-agent DaemonSet 的 namespace, port-forward 模式会用到
# 默认 'default'
debugAgentNamespace: kube-system
# 是否开启 port-forward 模式
# 默认 false
portForward: true
# image of the debug container
# default as showed
image: nicolaka/netshoot:latest
# start command of the debug container
# default ['bash']
command:
- '/bin/bash'
- '-l
```

> `kubectl-debug` 会将容器的 entrypoint 直接覆盖掉, 这是为了避免在 debug 时不小心启动非 shell 进程.
