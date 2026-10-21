OAPI_CODEGEN := go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.5.0

.PHONY: generate build run up down

generate:
	$(OAPI_CODEGEN) -config oapi-codegen.yaml api/openapi.yaml

build: generate
	go build ./...

run: generate
	go run ./cmd/server

up:
	docker compose up --build

down:
	docker compose down -v
