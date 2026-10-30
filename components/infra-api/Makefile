.PHONY: build test
build:
	@test ! -L .build-resource || (echo 'Refusing linked package directory'; exit 1)
	mkdir -p .build-resource
	@test ! -L .build-resource/server && (test ! -e .build-resource/server || test -f .build-resource/server) && test -z "$$(find .build-resource -mindepth 1 ! -name server -print -quit)" || (echo 'Package directory must contain only server; preserve and move unexpected files before rebuilding'; exit 1)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o .build-resource/server ./cmd/server

test:
	go test -race ./...
	go vet ./...
