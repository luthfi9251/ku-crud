dev-pg:
	docker compose up -d

dev:
	go run ./cmd/dev

test:
	go test ./...

build:
	cd web && npm ci && npm run build
	go build -o ku-crud .
