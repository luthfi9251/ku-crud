dev-pg:
	docker compose up -d

test:
	go test ./...
