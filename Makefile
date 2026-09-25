.PHONY: build check fmt hooks site verify

GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2

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
	$(GOLANGCI_LINT) run ./...
	go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
	pnpm --dir companion typecheck
	pnpm --dir companion lint
	pnpm --dir companion coverage
	pnpm --dir companion knip
	pnpm --dir companion hooks:check

fmt:
	$(GOLANGCI_LINT) fmt ./...
	pnpm --dir companion format

hooks:
	pnpm --dir companion exec lefthook install

site: build
	./bin/poolboy build
	rm -rf .site
	mkdir -p .site/docs
	cp site/obsidian.svg assets/poolboy-og.jpg .site/
	sed "s|{{site}}|$(SITE_URL)|g" site/index.html > .site/index.html
	cp README.md .site/install.md
	cp site/start.md site/try.md site/llms.txt .site/
	cp -R dist/. .site/docs/

verify:
	$(MAKE) check
	$(MAKE) site
	git diff --exit-code -- .poolboy/generated.json docs/reference/commands.md
	bash scripts/smoke.sh ./bin/poolboy
