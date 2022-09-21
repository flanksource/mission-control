
NAME=incident-commander
OS   = $(shell uname -s | tr '[:upper:]' '[:lower:]')
ARCH = $(shell uname -m | sed 's/x86_64/amd64/')
KUSTOMIZE=$(PWD)/.bin/kustomize

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

test: ui
	go test ./... -coverprofile cover.out

fmt:
	go fmt ./...

docker:
	docker build . -t ${IMG} --build-arg=GITHUB_TOKEN=$(GITHUB_TOKEN)

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

.PHONY: compress
compress: .bin/upx
	upx -5 ./.bin/$(NAME)_linux_amd64 ./.bin/$(NAME)_linux_arm64 ./.bin/$(NAME)_darwin_amd64 ./.bin/$(NAME)_darwin_arm64 ./.bin/$(NAME).exe

.PHONY: linux
linux: ui
	GOOS=linux GOARCH=amd64 go build  -o ./.bin/$(NAME)_linux_amd64 -ldflags "-X \"main.version=$(VERSION_TAG)\""  main.go
	GOOS=linux GOARCH=arm64 go build  -o ./.bin/$(NAME)_linux_arm64 -ldflags "-X \"main.version=$(VERSION_TAG)\""  main.go

.PHONY: darwin
darwin: ui
	GOOS=darwin GOARCH=amd64 go build -o ./.bin/$(NAME)_darwin_amd64 -ldflags "-X \"main.version=$(VERSION_TAG)\""  main.go
	GOOS=darwin GOARCH=arm64 go build -o ./.bin/$(NAME)_darwin_arm64 -ldflags "-X \"main.version=$(VERSION_TAG)\""  main.go

.PHONY: windows
windows: ui
	GOOS=windows GOARCH=amd64 go build -o ./.bin/$(NAME).exe -ldflags "-X \"main.version=$(VERSION_TAG)\""  main.go

.PHONY: binaries
binaries: linux darwin windows compress

.PHONY: release
release: binaries
	mkdir -p .release
	cp .bin/incident-commander* .release/

.PHONY: lint
lint:
	golangci-lint run

.PHONY: ui
ui:
	cd ui && npm ci && npm run build

.PHONY: build
build:
	go build -o ./.bin/$(NAME) -ldflags "-X \"main.version=$(VERSION_TAG)\""  main.go

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

.bin/kustomize: .bin
	curl -L https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv4.3.0/kustomize_v4.3.0_$(OS)_$(ARCH).tar.gz -o kustomize.tar.gz && \
    tar xf kustomize.tar.gz -C .bin/ && \
	rm kustomize.tar.gz


.PHONY: stack
stack: .bin/kustomize
	kubectl apply -f deploy/namespace.yaml
	$(KUSTOMIZE) build deploy/postgres | kubectl apply -f -
	kubectl wait --for=condition=ready pod -l app=postgres -n incident-commander --timeout=2m
	$(KUSTOMIZE) build deploy | kubectl apply -f -

.PHONY: chart-local
	helm dependency update ./chart
	cd chart && tar -xvf charts/kratos-0.25.*.tgz -C charts && rm charts/kratos-0.25.*.tgz && rm charts/kratos/templates/configmap-config.yaml && cd ..
	helm template -f ./chart/values.yaml incident-manager ./chart

.PHONY: chart
chart:
	helm dependency update ./chart
	helm dependency build ./chart
	cd chart && tar -xvf charts/kratos-0.25.*.tgz -C charts && rm charts/kratos-0.25.*.tgz && rm charts/kratos/templates/configmap-config.yaml && cd ..
	helm package ./chart
