.DEFAULT_GOAL := build

.PHONY: build
build:
	goreleaser release --snapshot --clean
