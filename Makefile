.PHONY: dev backend-build frontend-install start-vector-db reset

dev: start-vector-db backend-build frontend-install
	@echo "Starting backend and frontend..."
	@trap 'kill 0' INT TERM EXIT; \
		go run ./cmd/server & \
		(cd frontend && npm run dev)

backend-build:
	go mod download
	go build ./...

frontend-install:
	cd frontend && npm ci

start-vector-db:
	docker compose up -d db

reset:
	docker compose down -v
