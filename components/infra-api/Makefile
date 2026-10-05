.PHONY: build test
build:
	@test -n "$(INFRA_SCOPE_FILE)" || (echo 'Private scope file required'; exit 1)
	mkdir -p .build
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o .build/server ./cmd/server
	install -m 600 "$(INFRA_SCOPE_FILE)" .build/scope.json

test:
	go test -race ./...
	go vet ./...
