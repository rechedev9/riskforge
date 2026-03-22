.PHONY: build test vet check docker up down clean test-integration

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

test-integration:
	docker compose up -d spanner-emulator spanner-init
	@echo "Waiting for Spanner emulator..."
	@for i in $$(seq 1 30); do \
		curl -sf http://localhost:9020/v1/projects/riskforge-dev/instances/test-instance/databases/test-db > /dev/null 2>&1 && break; \
		sleep 2; \
	done
	SPANNER_EMULATOR_HOST=localhost:9010 \
	SPANNER_PROJECT=riskforge-dev \
	SPANNER_INSTANCE=test-instance \
	SPANNER_DATABASE=test-db \
	go test -tags integration -race -count=1 -v ./internal/adapter/spanner/...
