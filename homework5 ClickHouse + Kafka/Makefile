.PHONY: up down logs test test-local rebuild

up:
	docker compose up -d --build

down:
	docker compose down -v

logs:
	docker compose logs -f movie-service aggregation-service

test:
	docker compose --profile test run --rm --build integration-tests

test-local:
	go test -v -count=1 ./tests/...

rebuild:
	docker compose down -v && docker compose up -d --build
