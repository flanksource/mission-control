NAME=incident-commander
OS   = $(shell uname -s | tr '[:upper:]' '[:lower:]')
ARCH = $(shell uname -m | sed 's/x86_64/amd64/')
DATE = $(shell date  "+%Y-%m-%d %H:%M:%S")
ifeq ($(VERSION),)
  VERSION_TAG=$(shell git describe --abbrev=0 --tags --exact-match 2>/dev/null || echo latest)
else
  VERSION_TAG=$(VERSION)
endif

# Image URL to use all building/pushing image targets
IMG ?= docker.io/flanksource/$(NAME):${VERSION_TAG}

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/.bin
HOMEBREW_NODE_PATHS := /usr/local/opt/node/bin /opt/homebrew/opt/node/bin /opt/homebrew/opt/node@24/bin
export PATH := $(LOCALBIN):$(foreach p,$(HOMEBREW_NODE_PATHS),$(if $(wildcard $(p)/node),$(p):))$(PATH)
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
MODERNIZE ?= $(LOCALBIN)/modernize
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint

## Tool Versions
KUSTOMIZE_VERSION ?= any
CONTROLLER_TOOLS_VERSION ?= v0.19.0
GOLANGCI_LINT_VERSION ?= 2.11.4

TAILWIND_VERSION ?= 3.4.17
TAILWIND_JS = auth/oidc/static/tailwind.min.js

$(TAILWIND_JS):
	curl -sL "https://cdn.tailwindcss.com/$(TAILWIND_VERSION)" -o $(TAILWIND_JS)

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)


.PHONY: static
static: $(TAILWIND_JS) manifests generate fmt ginkgo ui

.PHONY: ui
ui: ## Build the embedded catalog explorer UI (ui/frontend -> ui/frontend/dist/ui.js)
	cd ui/frontend && CI=true pnpm install --no-frozen-lockfile --prefer-offline && pnpm run build

.PHONY: test
test:
	ginkgo -r --skip-package=tests/e2e --keep-going \
		--junit-report junit-report.xml \
		--github-output --output-dir test-reports \
		--succinct --label-filter='!ignore_local'

.PHONY: ci-test
ci-test: $(TAILWIND_JS) $(LOCALBIN)
	go build -o ./.bin/$(NAME) main.go
	ginkgo -r --skip-package=tests/e2e --keep-going --junit-report junit-report.xml --github-output --output-dir test-reports --succinct

.PHONY: e2e
e2e: $(TAILWIND_JS)
	go build -o ./.bin/$(NAME) main.go
	ginkgo -r --keep-going  ./tests/e2e/...

fmt:
	go fmt ./...

.PHONY: modernize
modernize: ## Run modernize against code.
	$(MODERNIZE) ./...

docker:
	docker build . -t ${IMG}

# Build the docker image
docker-dev: linux
	docker build ./ -f ./Dockerfile.dev -t ${IMG}

docker-push-%:
	docker build ./ -f ./Dockerfile.dev -t ${IMG}
	docker tag $(IMG) $*/$(IMG)
	docker push  $*/$(IMG)
	kubectl set image deployment/$(NAME) $(NAME)=$*/$(IMG)

# Push the docker image
docker-push:
	docker push ${IMG}


.PHONY: tidy
tidy:
	go mod tidy
	git add go.mod go.sum

.PHONY: compress
compress: .bin/upx
	upx -5 ./.bin/$(NAME)_linux_amd64 ./.bin/$(NAME)_linux_arm64

.PHONY: linux
linux: $(TAILWIND_JS)
	GOOS=linux GOARCH=amd64 go build  -o ./.bin/$(NAME)_linux_amd64 -ldflags "-X \"main.version=$(VERSION_TAG)\""  main.go
	GOOS=linux GOARCH=arm64 go build  -o ./.bin/$(NAME)_linux_arm64 -ldflags "-X \"main.version=$(VERSION_TAG)\""  main.go

.PHONY: darwin
darwin:
	GOOS=darwin GOARCH=amd64 go build -o ./.bin/$(NAME)_darwin_amd64 -ldflags "-X \"main.version=$(VERSION_TAG)\""  main.go
	GOOS=darwin GOARCH=arm64 go build -o ./.bin/$(NAME)_darwin_arm64 -ldflags "-X \"main.version=$(VERSION_TAG)\""  main.go

.PHONY: windows
windows:
	GOOS=windows GOARCH=amd64 go build -o ./.bin/$(NAME).exe -ldflags "-X \"main.version=$(VERSION_TAG)\""  main.go

.PHONY: binaries
binaries: linux darwin windows compress

.PHONY: release
release: binaries
	mkdir -p .release
	cp .bin/incident-commander* .release/

# Generate OpenAPI schema
.PHONY: gen-schemas
gen-schemas:
	cp go.mod hack/generate-schemas && \
	cd hack/generate-schemas && \
	go mod edit -module=github.com/flanksource/incident-commander/hack/generate-schemas && \
	go mod edit -require=github.com/flanksource/incident-commander@v1.0.0 && \
	go mod edit -replace=github.com/flanksource/incident-commander=../../ && \
	if grep -v "^//" ../../go.mod | grep -q "replace.*github.com/flanksource/duty.*=>"; then \
		go mod edit -replace=github.com/flanksource/duty=../../../duty; \
	fi && \
	if grep -v "^//" ../../go.mod | grep -q "replace.*github.com/flanksource/clicky.*=>"; then \
		go mod edit -replace=github.com/flanksource/clicky=../../../clicky; \
	fi && \
	go mod tidy && \
	go run ./main.go

.PHONY: manifests
manifests: generate gen-schemas ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) crd paths="./api/..." output:crd:artifacts:config=config/crds

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object paths="./api/..." paths="./logs/..."

.PHONY: build
build: static
	go build -o ./.bin/$(NAME) -ldflags "-X \"main.version=$(VERSION_TAG) built at $(DATE)\""  main.go

.PHONY: dev
dev: static
 	# Disabling CGO because of slow build times in apple silicon (just experimenting)
	CGO_ENABLED=0 go build -v -o ./.bin/$(NAME) -gcflags="all=-N -l" main.go

.PHONY: build-slim
build-slim: $(TAILWIND_JS) ## Fast go build only (no codegen or fmt)
	CGO_ENABLED=0 go build -o ./.bin/$(NAME) main.go

.PHONY: install
install:
	cp ./.bin/$(NAME) /usr/local/bin/

.PHONY: test-e2e
test-e2e: bin
	./test/e2e.sh

.bin/upx: .bin
	wget -nv -O upx.tar.xz https://github.com/upx/upx/releases/download/v3.96/upx-3.96-$(ARCH)_$(OS).tar.xz
	tar xf upx.tar.xz
	mv upx-3.96-$(ARCH)_$(OS)/upx .bin
	rm -rf upx-3.96-$(ARCH)_$(OS)

.bin:
	mkdir -p .bin


.PHONY: install-deps
install-deps: $(LOCALBIN) ## Install the deps CLI if not present
	which deps 2>/dev/null || test -x $(LOCALBIN)/deps || curl -sSL https://github.com/flanksource/deps/releases/latest/download/deps-$(OS)-$(ARCH).tar.gz | tar -xz -C $(LOCALBIN)

.PHONY: deps
deps: install-deps ginkgo controller-gen golangci-lint kustomize $(TAILWIND_JS) ## Install all tool dependencies

.PHONY: ginkgo
ginkgo:
	go install github.com/onsi/ginkgo/v2/ginkgo

.PHONY: controller-gen
controller-gen: install-deps $(LOCALBIN)
	deps install controller-gen@$(CONTROLLER_TOOLS_VERSION) --bin-dir $(LOCALBIN)

.PHONY: golangci-lint
golangci-lint: install-deps $(LOCALBIN)
	deps install golangci/golangci-lint@v$(GOLANGCI_LINT_VERSION) --bin-dir $(LOCALBIN)

.PHONY: kustomize
kustomize: install-deps $(LOCALBIN)
	deps install kubernetes-sigs/kustomize@$(KUSTOMIZE_VERSION) --bin-dir $(LOCALBIN)

.PHONY: docs\:mcp
docs\:mcp: ## Generate MCP tools reference documentation
	@mkdir -p docs
	go run ./hack/gen-mcp-docs > docs/mcp-tools.md
	@echo "Generated docs/mcp-tools.md"

report/kitchen-sink.json: report/build-kitchen-sink.ts report/testdata/kitchen-sink.yaml
	cd report && ./node_modules/.bin/tsx build-kitchen-sink.ts

.PHONY: lint
lint: golangci-lint
	$(GOLANGCI_LINT) run ./...
