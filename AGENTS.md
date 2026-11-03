# AGENTS.md

This file applies to the entire repository and guides coding agents working on this project.

## Project overview

- Kubernetes Cluster API Provider ELF (CAPE) provides infrastructure for Kubernetes clusters by managing ELF virtual machines through the SmartX CloudTower API.
- Go module: `github.com/smartxworks/cluster-api-provider-elf`.
- Built with Kubebuilder, controller-runtime, and Cluster API. The infrastructure API is `infrastructure.cluster.x-k8s.io/v1beta1`, with `ElfCluster`, `ElfMachine`, and `ElfMachineTemplate` as its core resources.
- The current Go version is `1.25.9`; refer to `.go-version`, `go.mod`, `Makefile`, and `Dockerfile`. Refer to `go.mod` for Cluster API, Kubernetes, and controller-runtime dependency versions.
- The project still uses CAPI v1beta1 APIs in many places, alongside v1beta2 in some integrations and tests. Preserve existing version compatibility and avoid unrelated API migrations.

## Repository layout

| Path | Responsibility |
| --- | --- |
| `main.go` | Manager entry point, command-line flags, controller and webhook registration |
| `api/v1beta1/` | CRD types, status, conditions, constants, and generated deepcopy code |
| `controllers/` | Cluster and machine reconciliation; VM resources, GPUs, cloud-init, placement groups, caching, and concurrency limits |
| `webhooks/` | Defaulting and validation for `ElfMachine` and `ElfMachineTemplate` |
| `pkg/service/` | `VMService` interface and CloudTower VM operations; generated mocks in `mock_services/` |
| `pkg/cloudtower/` | Tower clients and connection handling |
| `pkg/context/` | Operation contexts for managers, clusters, machines, and templates |
| `pkg/manager/` | Manager configuration, Secret caching, and read routing |
| `pkg/config/` | Environment variable configuration and defaults |
| `pkg/hostagent/` | Host Agent configuration and service integration |
| `pkg/util/` | Helpers for Kubernetes, CAPI, MachineDeployment, KubeadmControlPlane, and related logic |
| `config/` | CRD, RBAC, webhook, certificate, and deployment Kustomize configuration |
| `templates/` | clusterctl workload cluster templates |
| `test/helpers/`, `test/fake/` | envtest setup, test helpers, and Tower fakes |
| `test/e2e/` | End-to-end tests requiring Tower/ELF, environment configuration, and template sources |
| `hack/` | Scripts and templates for versioning, tooling, and Go copyright headers |
| `.github/workflows/` | CI checks and release workflows |

## Common commands

Run commands from the repository root. The Makefile installs pinned tool versions into `bin/`; initial setup may require network access.

| Command | Purpose and notes |
| --- | --- |
| `make help` | List available targets |
| `make build` | Generate code and manifests, then build `bin/manager` |
| `make generate` | Generate Go code, CRDs, RBAC, and webhook manifests |
| `make generate-go` | Run `go generate ./...` and generate deepcopy code |
| `make generate-manifests` | Generate CRD, RBAC, and webhook manifests from Kubebuilder markers |
| `make lint` | Run static checks using `.golangci.yml`, matching CI |
| `make test` | Generate code and manifests, prepare envtest, then test `api/`, `webhooks/`, `controllers/`, and `pkg/`; write coverage to `cover.out` |
| `make mockgen gen-mock` | Install mockgen and regenerate the VMService mock from `pkg/service/vm.go` |
| `make release-manifests` | Generate release manifests in `out/`, using `CONTROLLER_IMG` and `IMAGE_TAG` for the image |

The main CI checks are `make lint` and `make test`; see `.github/workflows/build.yml`.

## Testing strategy

- Start with tests for the affected packages. Add or update tests that cover the actual issue when behavior changes. Documentation-only changes usually need content and diff checks.
- Use targeted commands such as `go test ./pkg/service/...` for ordinary packages. Note that `make test` does not include `test/helpers/`; run `go test ./test/helpers/...` separately when changing that directory.
- The `TestMain` in `controllers/` starts a local API server, etcd, and manager. It requires envtest even when filtering tests with `-run`. The Makefile currently defaults to Kubernetes `1.33.0` test binaries.
- To run controller tests independently, prepare envtest and set the assets path first:

  ```bash
  make envtest
  export KUBEBUILDER_ASSETS="$(./bin/setup-envtest use 1.33.0 --bin-dir ./bin -p path)"
  go test ./controllers/...
  ```

- Controller tests use Ginkgo v2/Gomega; other packages also use standard `testing` tests. Follow the organization of nearby tests. For asynchronous reconciliation, use `Eventually` or similar assertions to wait for observable state rather than fixed sleeps.
- Reuse `test/fake/`, `test/helpers/`, and `pkg/service/mock_services/` to keep ordinary tests independent of a real Tower service.
- Avoid `go test ./...` as the default check: `test/e2e/` is not isolated by build tags, so recursive testing includes the E2E suite that requires real infrastructure.
- When the task requires E2E tests, read `test/e2e/README.md` and `test/e2e/config/elf-dev.yaml`, configure Tower, ELF, templates, and network resources, then run `make test-e2e`. This requires Docker/Kind and creates and cleans up clusters and virtual machines.
- If toolchain, network, or test environment limitations prevent validation, report the exact command, failure reason, and unverified scope. Do not report an environment failure as a passing test.

## Change conventions

- Keep changes focused and preserve the user's existing workspace changes. Check `git status` and `git diff` before and after work; do not overwrite unrelated changes.
- Format Go files with `gofmt` and follow nearby copyright headers, error handling, and logging patterns. Follow `.golangci.yml` for import grouping and aliases; common aliases include `goctx`, `corev1`, `metav1`, `clusterv1`, `clusterv2`, `infrav1`, and `ctrl`.
- Place new logic in the package responsible for it and reuse existing contexts, service interfaces, and helpers. Keep Tower operations testable through VMService or fakes/mocks.
- Reconciliation must be repeatable and correctly handle asynchronous tasks, retries, missing objects, and deletion. Follow existing patch helper, condition, finalizer, and pause conventions.
- Preserve existing JSON field names, defaults, and compatibility when changing APIs. Review related webhook validation, templates, and tests together. Use existing validation and tests as the reference for behavior such as resource expansion restrictions.
- After changing API types, Kubebuilder validation, or webhook/RBAC markers, run the relevant generation targets, review their output, and include the related tracked artifacts in the change.
- Do not directly edit generated code marked `DO NOT EDIT`, such as `api/v1beta1/zz_generated.deepcopy.go` or `pkg/service/mock_services/vm_mock.go`. Change the source definitions and regenerate. VMService interface changes require explicitly running the mock generation target.
- CAPE CRDs in `config/crd/bases/`, `config/rbac/role.yaml`, and `config/webhook/manifests.yaml` are maintained by generation. Handle external manifests such as Host Agent CRDs according to their source; do not assume controller-gen can rebuild every file.
- For E2E template changes, update `templates/cluster-template.yaml` or sources under `test/e2e/data/infrastructure-elf/kustomization/`, then run `make e2e-templates` to generate test templates.
- Do not commit local artifacts in `bin/`, `.build/`, `out/`, or `_artifacts/`, or test binaries, logs, and coverage files. Generation commands may modify tracked manifests; check the diff afterward.
- Change dependency or toolchain versions only when the task calls for it. When changing Go versions, check consistency across `.go-version`, `go.mod`, `Makefile`, and `Dockerfile`.

## Running and releasing

- `make run` connects to a cluster using kubeconfig. `make install`, `make deploy`, `make uninstall`, and `make undeploy` operate on the current Kubernetes context. Run them only when the task requires those environment operations, and verify the target context first.
- `make deploy` also modifies the tracked `config/default/manager_image_patch.yaml`; check that any resulting change is intended.
- `make release` builds and pushes amd64/arm64 images. Use `make release-manifests` when only local release manifests are needed. See `docs/releasing.md` and `.github/workflows/release.yml` for release details.
- Supply Tower passwords, tokens, kubeconfig, and Secret data through environment variables or local configuration. Do not put them in code, examples, or logs.
- When finishing a task, briefly describe the changes, validation performed, and anything left unverified.
