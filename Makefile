.PHONY: build run test vet docker

build:
	CGO_ENABLED=0 go build -trimpath -o bin/musicclub ./cmd/musicclub

run:
	DB_PATH=./club.db COOKIE_SECRET=dev-secret PORT=8080 go run ./cmd/musicclub

test:
	go test ./...

vet:
	go vet ./...

docker:
	docker build -t musicclub .
