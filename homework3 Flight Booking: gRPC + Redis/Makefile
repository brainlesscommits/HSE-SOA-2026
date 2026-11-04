CONTAINER ?= docker
PROTOC_IMAGE := golang:1.26-alpine

.PHONY: proto build test up down e2e

proto:
	tar -cf - proto | $(CONTAINER) run --rm -i $(PROTOC_IMAGE) sh -c '\
	  set -e; mkdir /src && cd /src && tar -xf - && mkdir gen && \
	  apk add --no-cache protobuf-dev >/dev/null && \
	  go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12 && \
	  go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2 && \
	  protoc -I proto -I /usr/include \
	    --go_out=gen --go_opt=paths=source_relative \
	    --go-grpc_out=gen --go-grpc_opt=paths=source_relative \
	    proto/flight/v1/flight.proto && \
	  tar -cf - gen' | tar -xf -

build:
	go build ./...

test:
	go test ./...

up:
	$(CONTAINER) compose up --build -d

down:
	$(CONTAINER) compose down -v
