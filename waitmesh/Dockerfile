FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /waitmesh ./cmd/waitmesh

FROM alpine:3.21
RUN adduser -D -H waitmesh \
  && apk add --no-cache ca-certificates wget
USER waitmesh
WORKDIR /data
COPY --from=build /waitmesh /usr/local/bin/waitmesh
EXPOSE 7777
# Non-loopback bind requires WAITMESH_TOKEN at runtime (enforced by serve Validate).
ENV WAITMESH_LISTEN=0.0.0.0:7777 \
    WAITMESH_DB=/data/waitmesh.db
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:7777/v1/health/live >/dev/null || exit 1
ENTRYPOINT ["waitmesh"]
CMD ["serve", "--listen", "0.0.0.0:7777", "--db", "/data/waitmesh.db"]
