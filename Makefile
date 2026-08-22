dev-pg:
	docker compose up -d

dev:
	go generate ./hooks
	go run ./cmd/main.go

test:
	go test ./...

build:
	cd web && npm ci && npm run build
	go generate ./hooks
	go build -o ku-crud ./cmd
	go build -o seed-admin ./cmd/seed-admin
