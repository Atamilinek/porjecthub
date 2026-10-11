.PHONY: test test-race vet build-web test-web typecheck-web

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

test-web:
	npm --prefix web run test:unit

typecheck-web:
	npm --prefix web run typecheck

build-web:
	npm --prefix web run build
