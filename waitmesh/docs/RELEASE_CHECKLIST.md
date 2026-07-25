# Waitmesh v0.1.0 release checklist

## Before tagging

- [ ] `go vet ./...` clean
- [ ] `go test ./...` pass
- [ ] `go test -race ./... -short` pass
- [ ] `go test ./internal/engine -run TestCrash_` pass
- [ ] `go run ./examples/payment` → DEMO OK
- [ ] CLI smoke: `serve`, `rules add`, `publish`, `pending list`
- [ ] Docker build + healthcheck with `WAITMESH_TOKEN` set
- [ ] README / OpenAPI match implemented routes
- [ ] CHANGELOG.md updated
- [ ] No `*.test` binaries committed
- [ ] Production binary built **without** `-tags crashinject`
- [ ] `docs/RELEASE_READINESS.md` reviewed (GO/NO-GO)

## Build / verify commands

```bash
cd waitmesh
go vet ./...
go test ./... -timeout 10m
go test -race ./... -short -timeout 15m
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/waitmesh ./cmd/waitmesh
./bin/waitmesh version   # expect 0.1.0
```

## Docker

```bash
docker build -t waitmesh:0.1.0 .
docker run --rm -e WAITMESH_TOKEN=dev -p 7777:7777 waitmesh:0.1.0
curl -s http://127.0.0.1:7777/v1/health/live
curl -s -H "Authorization: Bearer dev" http://127.0.0.1:7777/v1/rules
```

## Do not automate

- Do not `gh release create` / publish without human review of RELEASE_READINESS.md
