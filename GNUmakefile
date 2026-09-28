default: test

build:
	go build ./...

test:
	go test ./...

headers:
	go run ./tools/headers -fix

generate:
	go generate ./...

.PHONY: default build test headers generate
