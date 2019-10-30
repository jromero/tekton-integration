# tekton-integration

The purpose of this repository is solely to provide the means for testing the integration between [Tekton][tekton] and the [Lifecycle][lifecycle].

The Tekton task definition can be found at: https://github.com/tektoncd/catalog/blob/master/buildpacks

### Prerequisites

- Docker
- Git
- Go
- Kubectl

### Running tests

`go test -mod=vendor -v ./integration_test.go`

> For troubleshooting, clean up can be skipped via environment variable `SKIP_CLEANUP=true`.

[tekton]: https://tekton.dev/
[lifecycle]: https://buildpacks.io/docs/concepts/components/lifecycle/
