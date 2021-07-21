# Getting Started

This is a guide on how to get started with Cluster API Provider Elf. To learn more about cluster API in more
depth, check out the the [Cluster API book][cluster-api-book].

## Install Requirements

- clusterctl, which can downloaded the latest [release][releases] of Cluster API (CAPI) on GitHub.
- [Docker][docker] is required for the bootstrap cluster using `clusterctl`.
- [Kind][kind] can be used to provide an initial management cluster for testing.
- [kubectl][kubectl] is required to access your workload clusters.

### Elf Requirements

Your Elf environment should be configured with a **DHCP service** in the primary VM Network for your workload Kubernetes clusters.


## Creating a test management cluster

**NOTE**: You will need an initial management cluster to run the Cluster API components. This can be any 1.19+ Kubernetes cluster.
If you are testing locally, you can use [Kind][kind] with the following command:

```shell
kind create cluster
```

## Setting provider repository

To initialize Cluster API Provider Elf, you need to customize it as the provider
in the `~/.cluster-api/clusterctl.yaml` as the following:

```yaml
providers:
  - name: "elf"
    url: "https://github.com/smartxworks/cluster-api-provider-elf/releases/{latest|version-tag}/infrastructure-components.yaml"
    type: "InfrastructureProvider"
```

## Installing Cluster API Provider Elf in a management cluster

Once you have access to a management cluster, you can instantiate Cluster API with the following:

```shell
clusterctl init --infrastructure elf
```

## Configuring and creating a Elf-based workload cluster

To initialize workload cluster, you can provide values using OS environment variables as the following:

```shell
# The IP address or DNS of a ELF server
export ELF_SERVER=127.0.0.1

# The username used to access the ELF server
export ELF_SERVER_USERNAME=root

# The password used to access the ELF server
export ELF_SERVER_PASSWORD=root

# workload cluster name
export CLUSTER_NAME=cluster

# workload cluster kubernetes version
export KUBERNETES_VERSION=v1.20.6

# The IP to use as a control plane endpoint
export CONTROL_PLANE_ENDPOINT_IP=127.0.0.1

# The VM template to use for workload cluster
export ELF_TEMPLATE=336820d7-5ba5-4707-9d0c-8f3e583b950f
```

Then you can create workload cluster:

```shell
clusterctl generate cluster elf-quickstart \
    --infrastructure elf \
    --kubernetes-version v1.20.6 \
    --control-plane-machine-count 1 \
    --worker-machine-count 1 > cluster.yaml

# Inspect and make any changes
vi cluster.yaml

# Create the workload cluster in the current namespace on the management cluster
kubectl apply -f cluster.yaml
```

## Accessing the workload cluster

``` shell
clusterctl get kubeconfig elf-quickstart > elf-quickstart.kubeconfig
```

The kubeconfig can then be used to apply a CNI for networking, for example, Calico:

```shell
KUBECONFIG=elf-quickstart.kubeconfig kubectl apply -f https://docs.projectcalico.org/manifests/calico.yaml
```

After that you should see your nodes turn into ready:

```shell
KUBECONFIG=elf-quickstart.kubeconfig kubectl get nodes
NAME                                    STATUS     ROLES    AGE   VERSION
elf-quickstart-control-plane-6yljlb     Ready      master   6m    v1.20.6
```

## custom cluster templates

The provided cluster templates are quickstarts. If you need anything specific that requires a more complex setup, we recommand to use custom templates:

```shell
clusterctl generate yaml \
    --infrastructure elf \
    --kubernetes-version v1.20.6 \
    --control-plane-machine-count 1 \
    --worker-machine-count 1 \
    --from templates/cluster-template.yaml > cluster.yaml
```

<!-- References -->
[cluster-api-book]: https://cluster-api.sigs.k8s.io
[kind]: https://kind.sigs.k8s.io
[releases]: https://github.com/kubernetes-sigs/cluster-api/releases
[docker]: https://docs.docker.com/glossary/?term=install
[kubectl]: https://kubernetes.io/docs/tasks/tools/install-kubectl
