BINARY := agentctl
CMD    := ./cmd/agentctl

.PHONY: build
build:
	go build -o bin/$(BINARY) $(CMD)

.PHONY: test
test:
	go test ./...

.PHONY: test-integration
test-integration:
	AGENTCTL_INTEGRATION_INCUS=1 go test -tags=integration ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: fmt
fmt:
	gofmt -l .

.PHONY: lint
lint: vet fmt

.PHONY: docs
docs:
	mkdocs build --strict

.PHONY: docs-serve
docs-serve:
	mkdocs serve

.PHONY: clean
clean:
	rm -rf bin/ site/
