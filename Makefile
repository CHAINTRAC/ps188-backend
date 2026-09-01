.PHONY: run build test tidy fmt vet lint up down logs

run:
	go run ./cmd/api

build:
	go build -o bin/api ./cmd/api

# Tests run against a real MongoDB (TEST_MONGO_URI, default mongodb://localhost:27017);
# packages skip cleanly if none is reachable. GOTMPDIR is kept inside the repo so
# Windows Smart App Control does not block the compiled test binaries in %TEMP%.
test:
	mkdir -p .gotmp && GOTMPDIR=$(CURDIR)/.gotmp go test ./... -count=1

tidy:
	go mod tidy

fmt:
	gofmt -w .

vet:
	go vet ./...

# Local stack (Mongo + backend)
up:
	docker compose up --build -d

down:
	docker compose down

logs:
	docker compose logs -f backend
