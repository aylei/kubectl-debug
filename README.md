# Kubectl-debug

![license](https://img.shields.io/hexpm/l/plug.svg)
[![Go Report Card](https://goreportcard.com/badge/github.com/jamesTGrant/kubectl-debug)](https://goreportcard.com/report/github.com/jamesTGrant/kubectl-debug)
[![docker](https://img.shields.io/docker/pulls/jamesgrantmediakind/debug-agent.svg)](https://hub.docker.com/r/jamesgrantmediakind/debug-agent)


# Overview

`kubectl-debug` is an 'out-of-tree' solution for connecting to, and troubleshooting, an existing, running, 'target' container in an existing pod in a Kubernetes cluster.
The target container may have a shell and busybox utils and hence provide some debug capability. or it may be very minimal and not even provide a shell - which makes real-time troubleshooting very difficult. kubectl-debug is designed to overcome that difficulty.
  
How does it work?  
0 - User invokes kubectl-debug like this: `kubectl-debug --namespace NAMESPACE POD_NAME -c TARGET_CONTAINER_NAME`  
1 - kubectl-debug connects to kubectl and launches a new 'debug-agent' container on the same node as the 'target' container.  
2 - debug-agent container connects direct to containerd (or dockerd if applicable) on the host which is running the 'target' container and launches a new 'debug' container. in the same `pid`, `network`, `user` and `ipc` namespaces as the target container.  
3 - 'debug-agent' redirects the terminal output of the 'debug' container to the 'kubectl-debug' executable and so you can interact directly with the shell running in the debug container and so you can use all your favorite troubleshooting tools available in the debug container (BASH, cURL, tcpdump, etc) without the need to have these utilities in the target container image.  
  
kubectl-debug is not related to 'kubectl debug'
  
`kubectl-debug` has been replaced by kubernetes [ephemeral containers](https://kubernetes.io/docs/concepts/workloads/pods/ephemeral-containers). At the time of writing, ephemeral containers are still in alpha (Kubernetes current release is 1.22.4). You are required to explicitly enable alpha features (alpha features are not enabled by default). If you are using Azure AKS (and perhaps others) you are not able, nor permitted, to configure kubernetes feature flags and so you will need a solution like the one provided by this github project.


- [Kubectl-debug](#kubectl-debug)
- [Overview](#overview)
- [Quick Start](#quick-start)
  - [Install the kubectl debug plugin](#install-the-kubectl-debug-plugin)
  - [Debug instructions](#debug-instructions)
- [Build from source](#build-from-source)
- [port-forward mode and agentless mode(Default opening)](#port-forward-mode-and-agentless-modedefault-opening)
- [Configuration](#configuration)
- [Authorization](#authorization)
- [Roadmap](#roadmap)
- [Contribute](#contribute)
- [Acknowledgement](#acknowledgement)


# Quick Start

Download the binary (Linux only):
```bash
export RELEASE_VERSION=1.0.0
# linux x86_64
curl -Lo kubectl-debug.tar.gz https://github.com/JamesTGrant/kubectl-debug/releases/download/v${RELEASE_VERSION}/kubectl-debug_${RELEASE_VERSION}_linux_amd64.tar.gz

tar -zxvf kubectl-debug.tar.gz kubectl-debug
chmod +x kubectl-debug
sudo mv kubectl-debug /usr/local/bin/
```

## Usage instructions

Try it out!

```bash
# kubectl 1.12.0 or higher
kubectl-debug -h

# start the debug container in the same namespace, and cgroup etc as container 'CONTAINER_NAME' in pod 'POD_NAME' in namespace 'NAMESPACE'
kubectl-debug --namespace NAMESPACE POD_NAME -c TARGET_CONTAINER_NAME

# in case of your pod stuck in `CrashLoopBackoff` state and cannot be connected to,
# you can fork a new pod and diagnose the problem in the forked pod
kubectl-debug --namespace NAMESPACE POD_NAME -c CONTAINER_NAME --fork

# In 'fork' mode, if you want the copied pod to retain the labels of the original pod, you can use the --fork-pod-retain-labels parameter (comma separated, no spaces). If not set (default), this parameter is empty and so any labels of the original pod are not retained, and the labels of the copied pods are empty.
# Example of fork mode:
kubectl-debug --namespace NAMESPACE POD_NAME -c CONTAINER_NAME --fork --fork-pod-retain-labels=<labelKeyA>,<labelKeyB>,<labelKeyC>

# in order to interact with the debug-agent pod on a node which doesn't have a public IP or direct access (firewall and other reasons) to access, port-forward mode is enabled by default. if you don't want port-forward mode, you can use --port-forward false to turn off it. I don't know why you'd want to do this, but you can if you want.
kubectl-debug --port-forward=false --namespace NAMESPACE POD_NAME -c CONTAINER_NAME


# you can choose a different debug container image. By default, nicolaka/netshoot:latest will be used but you can specify anything you like
kubectl-debug --namespace NAMESPACE POD_NAME -c CONTAINER_NAME --image nicolaka/netshoot:latest 

# you can set the debug-agent pod's resource limits/requests, for example:
# default is not set
kubectl-debug --namespace NAMESPACE POD_NAME -c CONTAINER_NAME --agent-pod-cpu-requests=250m --agent-pod-cpu-limits=500m --agent-pod-memory-requests=200Mi --agent-pod-memory-limits=500Mi

# use primary docker registry, set registry kubernetes secret to pull image
# the default registry-secret-name is kubectl-debug-registry-secret, the default namespace is default
# please set the secret data source as {Username: <username>, Password: <password>}
kubectl-debug --namespace NAMESPACE POD_NAME --image nicolaka/netshoot:latest --registry-secret-name <k8s_secret_name> --registry-secret-namespace <namespace>


```

* You can configure the default arguments to simplify usage, refer to [Configuration](#configuration)

## (Optional) Create a Secret for Use with Private Docker Registries

You can use a new or existing [Kubernetes `dockerconfigjson` secret](https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/#registry-secret-existing-credentials). For example:

```bash
# Be sure to run "docker login" beforehand.
kubectl create secret generic kubectl-debug-registry-secret \
    --from-file=.dockerconfigjson=<path/to/.docker/config.json> \
    --type=kubernetes.io/dockerconfigjson
```

Alternatively, you can create a secret with the key `authStr` and a JSON payload containing a `Username` and `Password`. For example:

```bash
echo -n '{"Username": "calmkart", "Password": "calmkart"}' > ./authStr
kubectl create secret generic kubectl-debug-registry-secret --from-file=./authStr
```

Refer to [the official Kubernetes documentation on Secrets](https://kubernetes.io/docs/concepts/configuration/secret/) for more ways to create them.

# Build from source

Clone this repo and:
```bash
# make will build kubectl-debug binary and debug-agent image
make
# install plugin
mv kubectl-debug /usr/local/bin

# build debug-agent executable only - you wont need this. This is the executable that the debug-agent container contains. The dockerfile of the debug-agent container refers to this

# build agent only
make agent-docker
```

# port-forward mode And agentless mode(Default opening)

- `agentless` mode: By default, `debug-agent` will first start the `debug-agent` pod on the host where the target Pod is located, and then `debug-agent` pod will start the debug container. After the user exits, `kubectl-debug` will delete the debug container and `kubectl-debug` will delete the `debug-agent` pod.

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
# default registrySecretName is kubectl-debug-registry-secret
# default registrySecretNamespace is default
registrySecretName: my-debug-secret
registrySecretNamespace: debug
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
# You can disable SSL certificate check when communicating with image registry by 
# setting registrySkipTLSVerify to true.
registrySkipTLSVerify: false
# You can set the log level with the verbosity setting
verbosity : 0
```

PS: `kubectl-debug` will always override the entrypoint of the container, which is by design to avoid users running an unwanted service by mistake(of course you can always do this explicitly).

# Authorization

Currently, `kubectl-debug` reuse the privilege of the `pod/exec` sub resource to do authorization, which means that it has the same privilege requirements with the `kubectl exec` command.

# Auditing / Security

Some teams may want to limit what debug image users are allowed to use and to have an audit record for each command they run in the debug container.

You can use the environment variable ```KCTLDBG_RESTRICT_IMAGE_TO``` restrict the agent to using a specific container image.   For example putting the following in the container spec section of your daemonset yaml will force the agent to always use the image ```docker.io/nicolaka/netshoot:latest``` regardless of what the user specifies on the kubectl-debug command line 
```
          env : 
            - name: KCTLDBG_RESTRICT_IMAGE_TO
              value: docker.io/nicolaka/netshoot:latest
```
If ```KCTLDBG_RESTRICT_IMAGE_TO``` is set and as a result agent is using an image that is different than what the user requested then the agent will log to standard out a message that announces what is happening.   The message will include the URI's of both images.

Auditing can be enabled by placing 
```audit: true```
in the agent's config file.  

There are 3 settings related to auditing.
<dl>
<dt><code>audit</code></dt>
<dd>Boolean value that indicates whether auditing should be enabled or not.  Default value is <code>false</code></dd>
<dt><code>audit_fifo</code></dt>
<dd>Template of path to a FIFO that will be used to exchange audit information from the debug container to the agent.  The default value is <code>/var/data/kubectl-debug-audit-fifo/KCTLDBG-CONTAINER-ID</code>.   If auditing is enabled then the agent will :
<ol>
<li>Prior to creating the debug container, create a fifo based on the value of <code>audit_fifo</code>.  The agent will replace <code>KCTLDBG-CONTAINER-ID</code> with the id of the debug container it is creating.</li>
<li>Create a thread that reads lines of text from the FIFO and then writes log messages to standard out, where the log messages look similar to example below <br/>
<code>
2020/05/22 17:59:58 runtime.go:717: audit - user: USERNAME/885cbd0506868985a6fc491bb59a2d3c debugee: 48107cbdacf4b478cbf1e2e34dbea6ebb48a2942c5f3d1effbacf0a216eac94f exec: 265   execve("/bin/tar", ["tar", "--help"], 0x55a8d0dfa6c0 /* 7 vars */) = 0
</code><br/>
Where USERNAME is the kubernetes user as determined by the client that launched the debug container and debuggee is the container id of the container being debugged.
</li>
<li>Bind mount the fifo it creates to the debugger container.  </li>
</ol>
</dd>
<dt><code>audit_shim</code>
<dd>String array that will be placed before the command that will be run in the debug container.  The default value is <code>{"/usr/bin/strace", "-o", "KCTLDBG-FIFO", "-f", "-e", "trace=/exec"}</code>.  The agent will replace KCTLDBG-FIFO with the fifo path ( see above )  If auditing is enabled then agent will use the concatenation of the array specified by <code>audit_shim</code> and the original command array it was going to use.</dd>
</dl>

T
```
.

# Roadmap

`kubectl-debug` has been replaced by kubernetes [ephemeral containers](https://kubernetes.io/docs/concepts/workloads/pods/ephemeral-containers). At the time of writing, ephemeral containers are still in alpha (Kubernetes current release is 1.22.4). You are required to explicitly enable alpha features (alpha features are not enabled by default). If you are using Azure AKS (and perhaps others) you are not able, nor permitted, to configure kubernetes feature flags and so you will need a solution like the one provided by this github project.


# Contribute

Feel free to open issues and pull requests. Any feedback is highly appreciated!

# Acknowledgement

This project is a fork of (from what I think is abandonware) [this project](https://github.com/aylei/kubectl-debug) it would not be here without the effort of [aylei contributors](https://github.com/aylei/kubectl-debug/graphs/contributors), thanks!
