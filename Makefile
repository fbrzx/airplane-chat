.PHONY: dev backend-build frontend-install

dev: backend-build frontend-install
	@echo "Starting backend and frontend..."
	@trap 'kill 0' INT TERM EXIT; \
		go run ./cmd/server & \
		(cd frontend && npm run dev)

backend-build:
	go mod download
	go build ./...

frontend-install:
	cd frontend && npm install
