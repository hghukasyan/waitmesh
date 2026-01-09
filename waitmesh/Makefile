.PHONY: build test race bench demo run tidy vet release-check

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/waitmesh ./cmd/waitmesh

test:
	go test ./...

race:
	go test -race ./... -short

bench:
	go test ./benchmarks/... -bench=. -benchmem -count=1

demo:
	go run ./examples/payment

run: build
	./bin/waitmesh serve --db ./waitmesh.db

tidy:
	go mod tidy

vet:
	go vet ./...

release-check: vet test
	go test -race ./internal/... -short
	go test ./internal/engine -run TestCrash_ -count=1
	$(MAKE) build
	./bin/waitmesh version

