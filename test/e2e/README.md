# Testing

This document helps developers understand how to test CAPE.

## E2E

This section illustrates how to do end-to-end (e2e) testing with CAPE.

### Requirements

In order to run the e2e tests the following requirements must be met:

* Administrative access to a ELF server
* The testing must occur on a host that can access the VMs deployed to ELF via the network
* Ginkgo ([download](https://onsi.github.io/ginkgo/#getting-ginkgo))
* Docker ([download](https://www.docker.com/get-started))
* Kind v0.7.0+ ([download](https://kind.sigs.k8s.io))

### Environment variables

The first step to running the e2e tests is setting up the required environment variables:

| Environment variable | Description | Example |
| -------------------------- | ----------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `ELF_TEMPLATE` | The VM template to use for workload cluster | `336820d7-5ba5-4707-9d0c-8f3e583b950f`|
| `ELF_TEMPLATE_UPGRADE_TO` | The VM template to use for upgrading workload cluster | `c1347c27-ddd7-4b97-82e3-ca4e124623b4`|
| `CONTROL_PLANE_ENDPOINT_IP` | The IP that kube-vip is going to use as a control plane endpoint | `127.0.0.1`|
| `ELF_SERVER` | The IP address or DNS of a ELF server | `127.0.0.1`|
| `ELF_SERVER_USERNAME` | The username used to access the ELF server | `root`|
| `ELF_SERVER_PASSWORD` | The password used to access the ELF server | `root`|

### Running the e2e tests

Run the following command to execute the CAPE e2e tests:

```shell
make e2e
```

The above command should build the CAPE manager image locally and use that image with the e2e test suite.
