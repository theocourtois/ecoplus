.PHONY: check fmt lint nocomment test fuzz docs build

check: fmt lint nocomment test

fmt:
	gofmt -l -w .
	golangci-lint fmt

lint:
	golangci-lint run

nocomment:
	go run ./tools/nocomment ./...

test:
	go test -race -shuffle=on -count=1 ./...

fuzz:
	go test -run '^$$' -fuzz FuzzDecode -fuzztime 30s ./internal/labels

docs:
	go tool tfplugindocs generate --provider-name ecoplus

build:
	go build -o bin/terraform-provider-ecoplus .
	go build -o bin/ecoplus-server ./cmd/ecoplus-server
