SHELL := /bin/sh

.PHONY: preflight prepare build verify

preflight:
	@scripts/check-build-environment.sh

prepare: preflight
	@go mod download
	@CI=1 PNPM_DISABLE_SELF_UPDATE_CHECK=1 pnpm install --frozen-lockfile

build: prepare
	@go build ./...
	@pnpm typecheck

verify: prepare
	@go mod tidy -diff
	@go test -count=1 ./...
	@go vet ./...
	@pnpm typecheck
	@pnpm test
