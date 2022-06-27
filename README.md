# Deprecation Notice

This repository is no longer maintained, please checkout https://github.com/JamesTGrant/kubectl-debug.

# Kubectl-debug

![license](https://img.shields.io/hexpm/l/plug.svg)
[![Go Report Card](https://goreportcard.com/badge/github.com/jamesTGrant/kubectl-debug)](https://goreportcard.com/report/github.com/jamesTGrant/kubectl-debug)
[![docker](https://img.shields.io/docker/pulls/jamesgrantmediakind/debug-agent.svg)](https://hub.docker.com/r/jamesgrantmediakind/debug-agent)

- [Overview](#overview)
- [Quick start](#quick-start)
  - [Download the binary](#download-the-binary)
  - [Usage instructions](#usage-instructions)
  - [Build from source](#build-from-source)  
  - [Under the hood](#under-the-hood)
- [Configuration options and overrides](#configuration-options-and-overrides)
- [Authorization / required privileges](#authorization-required-privileges)
- [(Optional) Create a Secret for use with Private Docker Registries](#create-a-secret-for-use-with-private-docker-registries)
- [Roadmap](#roadmap)
- [Contribute](#contribute)
- [Acknowledgement](#acknowledgement)


# Overview

This project is a fork of this fine project: https://github.com/aylei/kubectl-debug which is no longer maintained (hence this fork). The credit for this project belongs with [aylei](https://github.com/aylei). Aylei and I have chatted and we are happy that this project will live on and get maintained here.

`kubectl-debug` is an 'out-of-tree' solution for connecting to and troubleshooting an existing, running, 'target' container in an existing pod in a Kubernetes cluster.
The target container may have a shell and busybox utils and hence provide some debug capability or it may be very minimal and not even provide a shell - which makes any real-time troubleshooting/debugging very difficult. kubectl-debug is designed to overcome that difficulty.

There's a short video on YouTube: https://www.youtube.com/watch?v=jJHCxCqPn1g

How does it work?  
<dd>
<ol>
<li> User invokes kubectl-debug like this: <code>kubectl-debug --namespace NAMESPACE POD_NAME -c TARGET_CONTAINER_NAME</code></li>
<li> kubectl-debug communicates with the cluster using the same interface as kubectl and instructs kubernetes to request the launch of a new 'debug-agent' container on the same node as the 'target' container </li>
<li> debug-agent process within the debug-agent pod connects directly to containerd (or dockerd if applicable) on the host which is running the 'target' container and requests the launch of a new 'debug' container in the same <code>pid</code>, <code>network</code>, <code>user</code> and <code>ipc</code> namespaces as the target container </li>
<li>In summary: 'kubectl-debug' causes the launch of the 'debug-agent' container, 'debug-agent' the causes the launch of the 'debug' pod/container </li>
<li> 'debug-agent' pod redirects the terminal output of the 'debug' container to the 'kubectl-debug' executable and so you can interact directly with the shell running in the 'debug' container. You can now use of the troubleshooting tools available in the debug container (BASH, cURL, tcpdump, etc) without the need to have these utilities in the target container image.</li>
</ol>
</dd>
  
`kubectl-debug` is not related to `kubectl debug`
  
`kubectl-debug` has been largely replaced by kubernetes [ephemeral containers](https://kubernetes.io/docs/concepts/workloads/pods/ephemeral-containers).  
 Ephemeral containers feature is in beta (enabled by default) from kubernetes 1.23  
 Ephemeral containers feature is in alpha from kubernetes 1.16 to 1.22  
 In Kuberenetes, by default, you are required to explicitly enable alpha features (alpha features are not enabled by default). If you are using Azure AKS (and perhaps others) you are not able, nor permitted, to configure kubernetes feature flags and so you will need a solution like the one provided by this github project.

# Quick start

## Download the binary 
(I'm testing Linux only):
```bash
export RELEASE_VERSION=1.0.0
# linux x86_64
curl -Lo kubectl-debug https://github.com/JamesTGrant/kubectl-debug/releases/download/v${RELEASE_VERSION}/kubectl-debug

# make the binary executable
chmod +x kubectl-debug

# run the binary pointing at whatever cluster kubectl points at
./kubectl-debug --namespace NAMESPACE TARGET_POD_NAME -c TARGET_CONTAINER_NAME
```
## Build from source

Clone this repo and:
```bash
# to use this kubectl-debug utility, you only need to take the resultant kubectl-debug binary 
# file which is created by:
make kubectl-debug-binary

# to 'install' the kubectl-debug binary, make it executable and either call it directy, put 
# it in your PATH, or move it to a location which is already in your PATH:

chmod +x kubectl-debug
mv kubectl-debug /usr/local/bin



#####################
# Extra options
######################

# build 'debug-agent' binary only - you wont need this. This is the binary/executable that 
# the 'debug-agent container' contains. 
# The dockerfile of the debug-agent container refers to this binary.
make debug-agent-binary

# build 'debug-agent' binary, and the 'debug-agent docker image'
# a docker image `jamesgrantmediakind/debug-agent:latest` will be created locally
make debug-agent-docker-image

# make everything; kubectl-debug-binary, debug-agent-binary, and 'debug-agent-docker-image'
make

```

## Usage instructions

```bash
# kubectl 1.12.0 or higher

# print the help
kubectl-debug -h

# start the debug container in the same namespace, and cgroup etc as container 'TARGET_CONTAINER_NAME' in
#  pod 'POD_NAME' in namespace 'NAMESPACE'
kubectl-debug --namespace NAMESPACE POD_NAME -c TARGET_CONTAINER_NAME

# in case of your pod stuck in `CrashLoopBackoff` state and cannot be connected to,
# you can fork a new pod and diagnose the problem in the forked pod
kubectl-debug --namespace NAMESPACE POD_NAME -c CONTAINER_NAME --fork

# In 'fork' mode, if you want the copied pod to retain the labels of the original pod, you can use 
# the --fork-pod-retain-labels parameter (comma separated, no spaces). If not set (default), this parameter 
# is empty and so any labels of the original pod are not retained, and the labels of the copied pods are empty.
# Example of fork mode:
kubectl-debug --namespace NAMESPACE POD_NAME -c CONTAINER_NAME --fork --fork-pod-retain-labels=<labelKeyA>,<labelKeyB>,<labelKeyC>

# in order to interact with the debug-agent pod on a node which doesn't have a public IP or direct access 
# (firewall and other reasons) to access, port-forward mode is enabled by default. If you don't want 
# port-forward mode, you can use --port-forward false to turn off it. I don't know why you'd want to do 
# this, but you can if you want.
kubectl-debug --port-forward=false --namespace NAMESPACE POD_NAME -c CONTAINER_NAME

# you can choose a different debug container image. By default, nicolaka/netshoot:latest will be 
# used but you can specify anything you like
kubectl-debug --namespace NAMESPACE POD_NAME -c CONTAINER_NAME --image nicolaka/netshoot:latest 

# you can set the debug-agent pod's resource limits/requests, for example:
# default is not set
kubectl-debug --namespace NAMESPACE POD_NAME -c CONTAINER_NAME --agent-pod-cpu-requests=250m --agent-pod-cpu-limits=500m --agent-pod-memory-requests=200Mi --agent-pod-memory-limits=500Mi

# use primary docker registry, set registry kubernetes secret to pull image
# the default registry-secret-name is kubectl-debug-registry-secret, the default namespace is default
# please set the secret data source as {Username: <username>, Password: <password>}
kubectl-debug --namespace NAMESPACE POD_NAME --image nicolaka/netshoot:latest --registry-secret-name <k8s_secret_name> --registry-secret-namespace <namespace>

# in addition to passing cli arguments, you can use a config file if you would like to 
# non-default values for various things.
kubectl-debug --configfile /PATH/FILENAME --namespace NAMESPACE POD_NAME -c TARGET_CONTAINER_NAME

```
## Debugging examples

This guide shows a few typical example of debugging a target container.

### Basic

When you run `kubectl-debug` it causes a 'debug container' to be created on the same node, and which runs in the same `pid`, `network`, `ipc` and `user` namespace, as the target container.
By default, `kubectl-debug` uses [`nicolaka/netshoot`](https://github.com/nicolaka/netshoot) as container image for the 'debug container'.
The netshoot [project documentation](https://github.com/nicolaka/netshoot/blob/master/README.md) provides excellent guides and examples for using various tools to troubleshoot your target container.

Here are a few examples to show `netshoot` working with `kubectl-debug`:

Connect to a running container 'demo-container' in pod 'demo-pod' in the default namespace:

```shell
➜  ~ kubectl-debug --namespace default target-pod -c target-container

Agent Pod info: [Name:debug-agent-pod-da46a000-8429-11e9-a40c-8c8590147766, Namespace:default, Image:jamesgrantmediakind/debug-agent:latest, HostPort:10027, ContainerPort:10027]
Waiting for pod debug-agent-pod-da46a000-8429-11e9-a40c-8c8590147766 to run...
pod target-pod pod IP: 10.233.111.78, agentPodIP 172.16.4.160
wait for forward port to debug agent ready...
Forwarding from 127.0.0.1:10027 -> 10027
Forwarding from [::1]:10027 -> 10027
Handling connection for 10027
                             pulling image nicolaka/netshoot:latest...
latest: Pulling from nicolaka/netshoot
Digest: sha256:5b1f5d66c4fa48a931ff54f2f34e5771eff2bc5e615fef441d5858e30e9bb921
Status: Image is up to date for nicolaka/netshoot:latest
starting debug container...
container created, open tty...

 [1] 🐳  → hostname
target-container
```
  
  
Navigating the filesystem of the target container:

The root filesystem of target container is located in `/proc/{pid}/root/`, and the `pid` is typically '1'. 
You can `chroot` to the root filesystem of target container to navigate the target container filesystem or
`cd /proc/1/root` works just as well (assuming PID '1' is the correct PID).

```shell
root @ /
 [2] 🐳  → chroot /proc/1/root

 root @ /
 [3] 🐳  → cd /proc/1/root
 
root @ /
 [#] 🐳  → ls
 bin            entrypoint.sh  home           lib64          mnt            root           sbin           sys            tmp            var
 dev            etc            lib            media          proc           run            srv            usr
 (you can navigate the target containers filesystem and view/edit files)

root @ /
 [#] 🐳  → ./entrypoint.sh
 (you can attempt to run the target containers entrypoint.sh script and perhaps see what errors are produced)
```
  
  
Using **iftop** to inspect network traffic:
```shell
root @ /
 [4] 🐳  → iftop -i eth0
interface: eth0
IP address is: 10.233.111.78
MAC address is: 86:c3:ae:9d:46:2b
(CLI graph omitted)
```
  
  
Using **drill** to diagnose DNS:
```shell
root @ /
 [5] 🐳  → drill -V 5 demo-service
;; ->>HEADER<<- opcode: QUERY, rcode: NOERROR, id: 0
;; flags: rd ; QUERY: 1, ANSWER: 0, AUTHORITY: 0, ADDITIONAL: 0
;; QUESTION SECTION:
;; demo-service.	IN	A

;; ANSWER SECTION:

;; AUTHORITY SECTION:

;; ADDITIONAL SECTION:

;; Query time: 0 msec
;; WHEN: Sat Jun  1 05:05:39 2019
;; MSG SIZE  rcvd: 0
;; ->>HEADER<<- opcode: QUERY, rcode: NXDOMAIN, id: 62711
;; flags: qr rd ra ; QUERY: 1, ANSWER: 0, AUTHORITY: 1, ADDITIONAL: 0
;; QUESTION SECTION:
;; demo-service.	IN	A

;; ANSWER SECTION:

;; AUTHORITY SECTION:
.	30	IN	SOA	a.root-servers.net. nstld.verisign-grs.com. 2019053101 1800 900 604800 86400

;; ADDITIONAL SECTION:

;; Query time: 58 msec
;; SERVER: 10.233.0.10
;; WHEN: Sat Jun  1 05:05:39 2019
;; MSG SIZE  rcvd: 121
```

### `proc` filesystem and FUSE

It is common to use tools like `top`, `free` to inspect system metrics like CPU usage and memory. Using these commands will display the metrics from the host system by default. Because they read the metrics from the `proc` filesystem (`/proc/*`), which is mounted from the host system. This can be extremely useful (you can still inspect the pod/container metrics of as part of the host metrics) You may find [this blog post](https://fabiokung.com/2014/03/13/memory-inside-linux-containers/) useful.

## Debug Pod in "CrashLoopBackoff"

Troubleshooting kubernetes containers in the  `CrashLoopBackoff` state can be tricky. Using kubectl-debug 'normally' probably wont help you as the debug container processed will be terminated reaped once the target container (process with pid 1) exits. To tackle with this, `kubectl-debug` provides the `--fork` flag, which borrows the idea from the `oc debug` command: copy the currently crashing pod and (hopefully) the issue will reproduce in the forked Pod with the added ability to debug via the debug container.

Under the hood, `kubectl debug --fork` will copy the entire Pod spec and:

* strip all the labels, so that no traffic will be routed from service to this pod (see[Readme.md](/README.md) for instructions on duplicating the labels);
* modify the entry-point of target container in order to hold the pid namespace and avoid the Pod crash again;

Here's an example:

```shell
➜  ~ kubectl-debug demo-pod -c demo-container --fork
Agent Pod info: [Name:debug-agent-pod-dea9e7c8-8439-11e9-883a-8c8590147766, Namespace:default, Image:jamesgrantmediakind/debug-agent:latest, HostPort:10027, ContainerPort:10027]
Waiting for pod debug-agent-pod-dea9e7c8-8439-11e9-883a-8c8590147766 to run...
Waiting for pod demo-pod-e23c1b68-8439-11e9-883a-8c8590147766-debug to run...
pod demo-pod PodIP 10.233.111.90, agentPodIP 172.16.4.160
wait for forward port to debug agent ready...
Forwarding from 127.0.0.1:10027 -> 10027
Forwarding from [::1]:10027 -> 10027
Handling connection for 10027
                             pulling image nicolaka/netshoot:latest...
latest: Pulling from nicolaka/netshoot
Digest: sha256:5b1f5d66c4fa48a931ff54f2f34e5771eff2bc5e615fef441d5858e30e9bb921
Status: Image is up to date for nicolaka/netshoot:latest
starting debug container...
container created, open tty...

 [1] 🐳  → ps -ef
PID   USER     TIME  COMMAND
    1 root      0:00 sh -c -- while true; do sleep 30; done;
    6 root      0:00 sleep 30
    7 root      0:00 /bin/bash -l
   15 root      0:00 ps -ef
```
  
  
## Debug init container

Just like debugging the ordinary container, we can debug the init-container of a pod. You must specify the container name of init-container:

```shell
➜  ~ kubectl-debug demo-pod -c init-container
```
  
  
# Under the hood

`kubectl-debug` consists of 3 components:

* the 'kubectl-debug' executable serves the `kubectl-debug` command and interfaces with the kube-api-server
* the 'debug-agent' pod is a temporary pod that is started in the cluster by kubectl-debug. The 'debug-agent' container is responsible for starting and manipulating the 'debug container'. The 'debug-agent' will also act as a websockets relay for remote tty to join the output of the 'debug container' to the terminal from which the kubectl-debug command was issued
* the 'debug container' which is the container that provides the debugging utilities and the shell in which the human user performs their debugging activity. `kubectl-debug` doesn't provide this - it's an 'off-the-shelf container image (nicolaka/netshoot:latest by default), it is invoked and configured by 'debug-agent'.

The following occurs when the user runs the command: `kubectl-debug --namespace <namespace> <target-pod> -c <container-name>` 

1. 'kubectl-debug' gets the target-pod info from kube-api-server and extracts the `host` information (the target-pod within the namespace <namespace>)
2. 'kubectl-debug' sends a 'debug-agent' pod specification to kube-api-server with a node-selector matching the `host`. By default the container image is `docker.io/jamesgrantmediakind/debug-agent:latest`
3. kube-api-server requests the creation of 'debug-agent' pod. 'debug-agent' pod is created in the default namespace (doesn't have to be the same namespace as the target pod)
4. 'kubectl-debug' sends an HTTP request to the 'debug-agent' pod running on the `host` which includes a protocol upgrade from HTTP to SPDY
5. debug-agent' checks if the target container is actively running, if not, write an error to client
6. 'debug-agent' interfaces with containerd (or dockerd if applicable) on the host to request the creation of the 'debug-container'. `debug container` with `tty` and `stdin` opened, the 'debug-agent' configures the `debug container`'s `pid`, `network`, `ipc` and `user` namespace to be that of the target container
7. 'debug-agent' pipes the connection into the `debug container` using `attach`
8. Human performs debugging/troubleshooting on the target container from 'within' in the debug container with access to the target container process/network/ipc namespaces and root filesystem
9. debugging complete, user exits the debug-container shell which closes the SPDY connection
10. 'debug-agent' closes the SPDY connection, then waits for the `debug container` to exit and do the cleanup
11. 'debug-agent' pod is deleted


# Configuration options and overrides

The `debug-agent` uses [nicolaka/netshoot](https://github.com/nicolaka/netshoot) as the default image to run debug container, and uses `bash` as default entrypoint. You can override the default image and entrypoint, as well as a number of other useful things, by passing the config file to the kubectl-debug command like this:
```bash
kubectl-debug --configfile CONFIGFILE --namespace NAMESPACE POD_NAME -c TARGET_CONTAINER_NAME
```
Example configfile:
```yaml
# debug agent listening port (outside container)
# default: 10027
agentPort: 10027

# namespace of debug-agent pod (does'nt need to be in the same namespace as the target container)
# default: 'default'
agentPodNamespace: default

# prefix of debug-agent pod
# default: 'debug-agent-pod'
agentPodNamePrefix: debug-agent-pod

# image of debug-agent pod
# default: jamesgrantmediakind/debug-agent:latest
agentImage: jamesgrantmediakind/debug-agent:latest

# auditing can be enabled by setting 'audit' to 'true'
# default: false
audit: false

# whether using port-forward when connecting debug-agent
# default: true
portForward: true

# the 'debug container' image
# default: nicolaka/netshoot:latest
# for most reliable result, use the full path - for example: docker.io/library/busybox:latest will
# work but busybox:latest may not (depending on the cluster)
image: nicolaka/netshoot:latest

# start command of the debug container
# `kubectl-debug` always specifies this explicitly and you can not override this without making code changes to `kubectl-debug`) this is by design.
# default ['bash']
command:
- '/bin/bash'
- '-l'

# private docker registry auth kubernetes secret
# default registrySecretName: kubectl-debug-registry-secret
# default registrySecretNamespace: default
registrySecretName: my-debug-secret
registrySecretNamespace: debug

# you can set the agent pod's resource limits/requests:
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

# You can set the debug logging output level with the verbosity setting. There are two levels of verbosity, 0 and any positive integer (ie; 'verbosity : 1' will produce the same debug output as 'verbosity : 5')
verbosity : 0
```

# Authorization / required privileges

Put simply - if you can successfully issue the command `kubectl exec` to a container in your cluster then `kubectl-debug` will work for you!
Detail: `kubectl-debug` reuses the privilege of the `pod/exec` sub-resource to do authorization, which means that it has the same privilege requirements as the `kubectl exec` command. 

The processes in the debug-agent container run as `root` and the debug-agent container `securityContext` is configured with `privileged: true` Some clusters such as OpenShift may not, by default, allow either of these practices.

# Auditing / Security

Some teams may want to limit what debug image users are allowed to use and to have an audit record for each command they run in the debug container.

You can add ```KCTLDBG_RESTRICT_IMAGE_TO``` to the config file to restrict the debug-agent to use a specific container image. For example putting the following in the config file will force the agent to always use the image ```docker.io/nicolaka/netshoot:latest``` regardless of what the user specifies on the kubectl-debug command line. This may be helpful for restrictive environments that mandate the use of ```KCTLDBG_RESTRICT_IMAGE_TO```
```
KCTLDBG_RESTRICT_IMAGE_TO: docker.io/nicolaka/netshoot:latest
```
If ```KCTLDBG_RESTRICT_IMAGE_TO``` is set and as a result agent is using an image that is different than what the user requested then the agent will log to standard out a message that announces what is happening.  The message will include the URI's of both images.

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

# (Optional) Create a Secret for Use with Private Docker Registries

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


# Roadmap

Jan '22 - plan to add support for k3s enviroments
March '22 - actually add support for k3s enviroments and auto LXCFS detection handling
  
`kubectl-debug` has been replaced by kubernetes [ephemeral containers](https://kubernetes.io/docs/concepts/workloads/pods/ephemeral-containers).
 Ephemeral containers feature is in beta from kubernetes 1.23
 Ephemeral containers feature is in alpha from kubernetes 1.16 to 1.22
  
 In Kuberenetes, by default, you are required to explicitly enable alpha features (alpha features are not enabled by default). If you are using Azure AKS (and perhaps others) you are not able, nor permitted, to configure kubernetes feature flags and so you will need a solution like the one provided by this github project.


# Contribute

Feel free to open issues and pull requests. Any feedback is much appreciated!

# Acknowledgement

This project is a fork of [this project](https://github.com/aylei/kubectl-debug) which is no longer maintained (hence this fork).
This project would not be here without the effort of [aylei contributors](https://github.com/aylei/kubectl-debug/graphs/contributors) and [this fork](https://github.com/JamesTGrant/kubectl-debug/graphs/contributors)
