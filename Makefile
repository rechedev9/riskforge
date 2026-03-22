.PHONY: build test vet check docker up down clean

build:
	CGO_ENABLED=0 go build -o bin/api ./cmd/api

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

check: vet test

docker:
	docker build -t riskforge-api .

up:
	docker compose up --build -d

down:
	docker compose down

clean:
	rm -rf bin/
