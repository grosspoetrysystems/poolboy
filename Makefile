.PHONY: build check fmt hooks site

# SITE_URL absolutizes the product page's share-card and canonical URLs
# (e.g. https://poolboy.example). Empty keeps them root-relative to the host.
SITE_URL ?=

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

site: build
	./bin/poolboy build
	rm -rf .site
	mkdir -p .site/docs
	cp site/obsidian.svg assets/poolboy-og.jpg .site/
	sed "s|{{site}}|$(SITE_URL)|g" site/index.html > .site/index.html
	cp README.md .site/install.md
	cp site/start.md site/try.md site/llms.txt .site/
	cp -R dist/. .site/docs/
