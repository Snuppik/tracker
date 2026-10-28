.PHONY: run build test lint cover reset-db test-db logs psql genvapid demo

run:        ; go run ./cmd/api
build:      ; CGO_ENABLED=0 go build -o bin/tracker ./cmd/api
test:       ; go test ./... -count=1
lint:       ; test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1); go vet ./...
cover:      ; go test ./... -coverprofile=coverage.out && go tool cover -func=coverage.out
reset-db:   ; docker compose down -v && docker compose up -d postgres
test-db:    ; docker compose exec -T postgres psql -U tracker -d tracker -c "CREATE DATABASE tracker_test" || true
logs:       ; docker compose logs -f api
psql:       ; docker compose exec postgres psql -U tracker -d tracker
genvapid:   ; go run ./cmd/genvapid
demo:       ; docker compose down -v && docker compose up -d --build
