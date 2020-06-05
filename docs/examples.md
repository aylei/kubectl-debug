# Debugging examples

This guide will walk-through the typical debugging workflow of `kubectl-debug`.

> **Note:** The rest of this document assumes you have installed and properly configured `kubectl-debug` according to the [Project REAMDE](/README.md).

If you have any real world examples to share with `kubectl-debug`, feel free to open a pull request.

Here's the config file for the following commands for you to re-produce all the command outputs:

```yaml
agent_port: 10027
portForward: true
agentless: true
command:
- '/bin/bash'
- '-l'
```

## Basic

`kubectl-debug` use [`nicolaka/netshoot`](https://github.com/nicolaka/netshoot) as the default debug image, the [project document](https://github.com/nicolaka/netshoot/blob/master/README.md) is a great guide about using various tools to troubleshoot your container network. 

We will take a few examples here to show how does the powerful `netshoot` work in the `kubectl-debug` context:

Connect to pod:

```shell
➜  ~ kubectl debug demo-pod

Agent Pod info: [Name:debug-agent-pod-da46a000-8429-11e9-a40c-8c8590147766, Namespace:default, Image:aylei/debug-agent:latest, HostPort:10027, ContainerPort:10027]
Waiting for pod debug-agent-pod-da46a000-8429-11e9-a40c-8c8590147766 to run...
pod demo-pod PodIP 10.233.111.78, agentPodIP 172.16.4.160
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
demo-pod
```

Using **iftop** to inspect network traffic:
```shell
root @ /
 [2] 🐳  → iftop -i eth0
interface: eth0
IP address is: 10.233.111.78
MAC address is: 86:c3:ae:9d:46:2b
(CLI graph omitted)
```

Using **drill** to diagnose DNS:
```shell
root @ /
 [3] 🐳  → drill -V 5 demo-service
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

It is common to use tools like `top`, `free` to inspect system metrics like CPU usage and memory. Unfortunately, these commands will display the metrics from the host system by default. Because they read the metrics from the `proc` filesystem (`/proc/*`), which is mounted from the host system.

While this is acceptable (you can still inspect the metrics of container process in the host metrics), this can be misleading and 
counter-intuitive. A common solution is using a [FUSE](https://en.wikipedia.org/wiki/Filesystem_in_Userspace) filesystem, which is out of the scope of `kubectl-debug` plugin.

You may find [this blog post](https://fabiokung.com/2014/03/13/memory-inside-linux-containers/) useful if you want to investigate this problem in depth.

## Access the root filesystem of target container

The root filesystem of target container is located in `/proc/{pid}/root/`, and the `pid` is 1 typically (Pod with [`sharingProcessNamespace`](https://kubernetes.io/docs/tasks/configure-pod-container/share-process-namespace/) enabled is an exception).

```shell
root @ /
 [4] 🐳  → tail /proc/1/root/log_
Hello, world!
```

## Debug Pod in "CrashLoopBackoff"

Troubleshooting `CrashLoopBackoff` of Kubernetes Pod can be tricky. The debug container process will be reaped once the target container (process with pid 1) exists. To tackle with this, `kubectl-debug` provides the `--fork` flag, which borrow the idea from the `oc debug` command: copy the currently Pod and re-produce the issue in the forked Pod.

Under the hood, `kubectl debug --fork` will copy the entire Pod spec and:

* strip all the labels, so that no traffic will be routed from service to this pod;
* modify the entry-point of target container in order to hold the pid namespace and avoid the Pod crash again;

Here's an example:

```shell
➜  ~ kubectl debug demo-pod --fork
Agent Pod info: [Name:debug-agent-pod-dea9e7c8-8439-11e9-883a-8c8590147766, Namespace:default, Image:aylei/debug-agent:latest, HostPort:10027, ContainerPort:10027]
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

You can `chroot` to the root filesystem of target container to re-produce the error that causes the Pod to crash:

```shell
root @ /
 [4] 🐳  → chroot /proc/1/root
 
root @ /
 [#] 🐳  → ls
 bin            entrypoint.sh  home           lib64          mnt            root           sbin           sys            tmp            var
 dev            etc            lib            media          proc           run            srv            usr
 
root @ /
 [#] 🐳  → ./entrypoint.sh
 (...errors)
```

## Debug init container

Just like debugging the ordinary container, we can debug the init-container of Pod. In this case, you must specify the container name of init-container:

```shell
➜  ~ kubectl debug demo-pod --container=init-pod
```
