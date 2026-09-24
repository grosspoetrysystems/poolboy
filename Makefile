.PHONY: build check fmt hooks

build:
	pnpm --dir companion build
	mkdir -p bin
	go build -trimpath -o bin/poolboy ./cmd/poolboy
	cp companion/dist/cli.mjs bin/poolboy-knap.mjs
	cp LICENSE companion/THIRD_PARTY_NOTICES bin/

check:
	go test -race -cover ./...
	golangci-lint run ./...
	pnpm --dir companion typecheck
	pnpm --dir companion lint
	pnpm --dir companion coverage
	pnpm --dir companion knip

fmt:
	golangci-lint fmt ./...
	pnpm --dir companion format

hooks:
	lefthook install
