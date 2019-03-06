# Kubectl-debug

[Kubectl-debug](https://github.com/aylei/kubectl-debug) is an out-of-tree solution for troubleshooting running pods, which allows you to run a new container in running pods for debugging purpose

Documentation is available at https://github.com/aylei/kubectl-debug

## TL;DR;

```console
cd contrib/helm/kubectl-debug
helm install .
```

## Introduction

This chart bootstraps a [Kubectl-debug](https://github.com/aylei/kubectl-debug) deployment of agent on a [Kubernetes](http://kubernetes.io) cluster using the [Helm](https://helm.sh) package manager.

## Prerequisites

- Kubernetes 1.4+ with Beta APIs enabled

## Installing the Chart

To install the chart with the release name `my-release`:

```console
# Inside of kubectl-debug/contrib/helm/kubectl-debug
helm install --name my-release . 
```

> **Tip**: List all releases using `helm list`

## Uninstalling the Chart

To uninstall/delete the `my-release` deployment:

```console
$ helm delete my-release --purge
```

The command removes all the Kubernetes components associated with the chart and deletes the release.

## Configuration

Please refer to default values.yaml and source code
Specify each parameter using the `--set key=value[,key=value]` argument to `helm install`. 

Alternatively, a YAML file that specifies the values for the above parameters can be provided while installing the chart. For example,

```console
$ helm install --name my-release -f values.yaml .
```

> **Tip**: You can use the default [values.yaml](values.yaml)

## Image

The `image` parameter allows specifying which image will be pulled for the chart.
