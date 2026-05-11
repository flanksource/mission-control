PLUGIN_NAME := kubernetes-logs
PLUGIN_PATH ?= $(MISSION_CONTROL_PLUGIN_PATH)
PLUGIN_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
VERSION ?= dev
BUILD_DATE := $(shell date "+%Y-%m-%d %H:%M:%S")

.PHONY: build
build:
	@test -n "$(PLUGIN_PATH)" || (echo "MISSION_CONTROL_PLUGIN_PATH is required"; exit 1)
	mkdir -p "$(PLUGIN_PATH)"
	go build \
		-ldflags "-X 'main.Version=$(VERSION)' -X 'main.BuildDate=$(BUILD_DATE)'" \
		-o "$(PLUGIN_PATH)/$(PLUGIN_NAME)" \
		"$(PLUGIN_DIR)"

.PHONY: install
install: build
	kubectl apply -f "$(PLUGIN_DIR)/Plugin.yaml"
