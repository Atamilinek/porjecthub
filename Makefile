.PHONY: test test-race vet build-web test-web typecheck-web smoke-local

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
	mkdir -p internal/webapp/dist
	cp -R web/dist/. internal/webapp/dist/

smoke-local:
	bash scripts/smoke-local.sh
