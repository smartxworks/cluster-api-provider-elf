# Kubernetes Cluster API Provider Elf

[![build](https://github.com/smartxworks/cluster-api-provider-elf/actions/workflows/build.yml/badge.svg)](https://github.com/smartxworks/cluster-api-provider-elf/actions/workflows/build.yml)

<a href="https://github.com/kubernetes/kubernetes"><img src="https://github.com/kubernetes/kubernetes/raw/master/logo/logo.png" width="100" height="100" /></a><a href="https://www.smartx.com"><img height="100" hspace="90px" src="https://avatars.githubusercontent.com/u/13521339" alt="Powered by Elf" /></a>

------

Kubernetes-native declarative infrastructure for Elf.

## What is the Cluster API Provider Elf

The [Cluster API][cluster_api] brings declarative, Kubernetes-style APIs to cluster creation, configuration and management. Cluster API Provider for Elf is a concrete implementation of Cluster API for Elf.

The API itself is shared across multiple cloud providers allowing for true Elf hybrid deployments of Kubernetes. It is built atop the lessons learned from previous cluster managers such as [kops][kops] and [kubicorn][kubicorn].

## Launching a Kubernetes cluster on Elf

Check out the [getting started guide](./docs/getting_started.md) for launching a cluster on Elf.

## Features

- Native Kubernetes manifests and API
- Manages the bootstrapping of VMs on cluster.
- CentOS 7 using VM Templates.
- Deploys Kubernetes control planes into provided clusters on Elf.
- Doesn't use SSH for bootstrapping nodes.
- Installs only the minimal components to bootstrap a control plane and workers.

------

## Compatibility with Cluster API and Kubernetes Versions

This provider's versions are compatible with the following versions of Cluster API:

|                              | Cluster API v1alpha3 (v0.3) | Cluster API v1alpha4 (v0.4) |
| ---------------------------- | :---------------------------: | :---------------------------: |
| Elf Provider v1alpha4 (v0.1, master) |              ☓              |              ✓              |


This provider's versions are able to install and manage the following versions of Kubernetes:

|                              | Kubernetes 1.19 | Kubernetes 1.20 | Kubernetes 1.21 |
| ---------------------------- | :---------------: | :---------------: | :---------------: |
| Elf Provider v1alpha4 (v0.1, master) |        ✓          |         ✓         |         ✓         |

**NOTE:** As the versioning for this project is tied to the versioning of Cluster API, future modifications to this policy may be made to more closely align with other providers in the Cluster API ecosystem.

## Documentation

Further documentation is available in the [docs](./docs) directory.

<!-- References -->
[cluster_api]: https://github.com/kubernetes-sigs/cluster-api
[kops]: https://github.com/kubernetes/kops
[kubicorn]: http://kubicorn.io/
