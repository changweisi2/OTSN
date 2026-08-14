BIN     := otsn
VERSION := 0.1.0
PREFIX  ?= /usr/local
LDFLAGS := -s -w -X otsn/internal/app.Version=$(VERSION)

.PHONY: build test vet lint release install clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BIN) .

install:
	@test -f bin/$(BIN) || (echo "run 'make build' first" && exit 1)
	install -m 755 bin/$(BIN) $(PREFIX)/bin/$(BIN)

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed; 'make vet' covers the basics"

release:
	@mkdir -p dist
	@for t in "linux amd64" "linux arm64" "darwin amd64" "darwin arm64" "windows amd64" "windows arm64"; do \
		set -- $$t; \
		GOOS=$$1 GOARCH=$$2 CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' \
			-o dist/$(BIN)-$$1-$$2$$([ "$$1" = windows ] && echo .exe) .; \
	done
	@ls -lh dist

clean:
	rm -rf bin dist
