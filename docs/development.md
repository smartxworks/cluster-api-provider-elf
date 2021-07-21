# Development

## Developing in conjunction with clusterctl

If you want to use clusterctl with development work, or when working from master,
you will need the following:

* jq
* kind
* Docker
* An OCI registry

### Configuration

Set the `DEV_REGISTRY` environment variable to the OCI repository
you want test images uploaded to.

The following environment variables are supported for configuration:

```shell
export VERSION ?= $(shell cat clusterctl-settings.json | jq .config.nextVersion -r)
export IMAGE_NAME ?= manager
export PULL_POLICY ?= Always
export DEV_REGISTRY ?= gcr.io/$(shell gcloud config get-value project)
export DEV_CONTROLLER_IMG ?= $(DEV_REGISTRY)/elf-$(IMAGE_NAME)
export DEV_TAG ?= dev
```

### Building images

The following make targets build and push a test image to your repository:

``` shell
make docker-build

make docker-push
```

### Generating clusterctl overrides

Overrides need to be placed in `~/.cluster-api/overrides/infrastructure-elf/{version}`

Running the following generates these overrides for you:

``` shell
make dev-manifests
```

For development purposes, if you are using a `major.minor.x` version (see env variable VERSION) which is not present in the `metadata.yml`, include this version entry along with the API contract in the file.

### Using dev manifests

After publishing your test image and generating the manifests, you can use
`clusterctl`, as per the getting-started instructions.

## Testing e2e

See the [e2e docs](../test/e2e/README.md)
