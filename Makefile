SHELL = /bin/bash
.SHELLFLAGS: -o pipefail -c -e

COVERPROFILE?=coverage.out
UPLOAD_COVERAGE?=false

.PHONY: deps
deps:
	go mod tidy
	go get -u ./...

.PHONY: lint
lint:
	golangci-lint run --verbose ./...

.PHONY: fmt
fmt:
	golangci-lint run --fix --verbose ./...

.PHONY: test
test:
	go test -v -race -coverprofile=$(COVERPROFILE) ./...
	go tool cover -html=$(COVERPROFILE) -o coverage.html
	@if [[ -n "$$CI" && "$(UPLOAD_COVERAGE)" == "true" ]]; then bash <(curl -s https://codecov.io/bash) -f $(COVERPROFILE); fi

.PHONY: build
build:
	go build -v -o /dev/null ./...