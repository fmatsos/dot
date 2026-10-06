.PHONY: test build

test:
	go vet ./... && go test -race ./...

build:
	CGO_ENABLED=0 go build -trimpath -o dot ./cmd/dot
