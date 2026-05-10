PLUGIN_NAME := kubernetes-logs
PLUGIN_PATH ?= $(MISSION_CONTROL_PLUGIN_PATH)
VERSION ?= dev
BUILD_DATE := $(shell date "+%Y-%m-%d %H:%M:%S")

.PHONY: build
build:
	@test -n "$(PLUGIN_PATH)" || (echo "MISSION_CONTROL_PLUGIN_PATH is required"; exit 1)
	mkdir -p "$(PLUGIN_PATH)"
	go build \
		-ldflags "-X 'main.Version=$(VERSION)' -X 'main.BuildDate=$(BUILD_DATE)'" \
		-o "$(PLUGIN_PATH)/$(PLUGIN_NAME)" \
		./plugins/kubernetes-logs

.PHONY: install
install: build
	kubectl apply -f plugins/kubernetes-logs/Plugin.yaml
