.PHONY: build run test vet docker

build:
	CGO_ENABLED=0 go build -trimpath -o bin/musiclub ./cmd/musiclub

run:
	DB_PATH=./club.db COOKIE_SECRET=dev-secret PORT=8080 go run ./cmd/musiclub

format:
	go tool gofumpt -l -w .

test:
	go test ./...

docker:
	docker build -t musiclub .

clean:
	rm -rf bin
	rm -f club.db
