COMMIT = $(shell git describe --dirty --always)
BRANCH = $(shell git rev-parse --abbrev-ref HEAD)


help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[32m%-30s\033[0m %s\n", $$1, $$2}'


#####################
# Build and cleanup #
#####################


.PHONY: build
build:  ## Run go build for speaker and controller
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -v -o build/amd64/controller/controller -ldflags '-X go.universe.tf/metallb/internal/version.gitCommit=${COMMIT} -X go.universe.tf/metallb/internal/version.gitBranch=${BRANCH}' go.universe.tf/metallb/controller
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -v -o build/amd64/speaker/speaker -ldflags '-X go.universe.tf/metallb/internal/version.gitCommit=${COMMIT} -X go.universe.tf/metallb/internal/version.gitBranch=${BRANCH}' go.universe.tf/metallb/speaker


.PHONY: test
test:  ## Run unit tests
	go test ./... -short

.PHONY: test-local
test-local:  ## Run unit tests locally, we need to do it in a container because weird macOS vs. Linux stuff
	docker run -v ${PWD}:/metallb -w /metallb golang:1.13 make test