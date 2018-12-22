# Kubectl debug

`kubectl-debug` 是一个简单的 kubectl 插件, 能够帮助你便捷地进行 Kubernetes 上的 Pod 排障诊断. 背后做的事情很简单: 在运行中的 Pod 上额外起一个新容器, 并将新容器加入到目标容器的 `pid`, `network`, `user` 以及 `ipc` namespace 中, 这时我们就可以在新容器中直接用 `netstat`, `tcpdump` 这些熟悉的工具来解决问题了, 而旧容器可以保持最小化, 不需要预装任何额外的排障工具.

# Quick Start

`kubectl-debug` 包含两部分, 一部分是用户侧的 kubectl 插件, 另一部分是部署在所有 k8s 节点上的 agent(用于启动"新容器", 同时也作为 SPDY 连接的中继). 因此首先要部署 agent.

推荐以 DaemonSet 的形式部署:
```bash
kubectl apply -f https://raw.githubusercontent.com/aylei/kubectl-debug/master/scripts/agent_daemonset.yml
```

接下来, 安装 kubectl 插件:


装完之后就可以试试看了:
```bash
kubectl debug POD_NAME
# learn more with 
kubectl debug -h
```

# 默认镜像和 Entrypoint

`kubectl-debug` 使用 [nicolaka/netshoot](https://github.com/nicolaka/netshoot) 作为默认镜像. 默认镜像和指令都可以通过命令行参数进行覆盖. 考虑到每次都指定有点麻烦, 也可以通过文件配置的形式进行覆盖, 编辑 `~/.kube/debug-config` 文件:

```bash
agent_port: 10027
image: nicolaka/netshoot:latest
command:
- '/bin/bash'
- '-l'
```

> `kubectl-debug` 会将容器的 entrypoint 直接覆盖掉, 这是为了避免在 debug 时不小心启动非 shell 进程.

# 实现细节

主要参照了 `kubectl exec` 的实现, 但 `exec` 要复杂很多, `debug` 的链路还是很简单的:

1. 根据指令找到 Pod 的 HostIP, 以及目标容器的 ContainerID
2. 对 HostIP 上的 agent 发起 http 请求, 请求会携带上 image, command 这些参数
3. agent 返回一个协议升级响应, 在 client 与 agent 之间建立 SPDY 连接
4. agent 确认目标容器是否正常运行, 若否, 返回一个错误
5. agent 启动一个新容器, 加入到目标容器的 `pid`, `network`, `ipc` 以及 `user` namespace 中
6. 容器启动完成后, agent 将 server 侧 SPDY 的 stdin, stdout, tty 绑定到新容器的 stdin, stdout 和 tty 上
7. 一切就绪, 可以开始 debug 了
8. debug 完毕, 关闭连接, agent 做一些清理操作, 关闭并移除容器