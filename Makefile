GO ?= go
UV ?= uv

.PHONY: build test helper-test check race pty demo

build:
	$(GO) build -o bin/lazycred ./cmd/lazycred

test:
	$(GO) test ./...

helper-test:
	$(UV) run --no-project --with yubikey-manager==5.9.2 --with pyotp==2.9.0 python -m unittest discover -s tests -v

check: test helper-test
	$(GO) vet ./...
	@test -z "$$(gofmt -l cmd internal)"
	git diff --check

race:
	$(GO) test -race ./...

pty: build
	$(UV) run --script scripts/pty_smoke.py bin/lazycred

demo: build
	./bin/lazycred --demo
