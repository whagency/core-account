# Makefile for account-service

GOVULNCHECK := go run golang.org/x/vuln/cmd/govulncheck@v1.1.4
GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4
SWAG := go run github.com/swaggo/swag/cmd/swag@v1.16.6

# Check for vulnerable dependencies using govulncheck.
.PHONY: vulnerable-check
vulnerable-check:
	$(GOVULNCHECK) ./...

# Linting using golangci-lint.
.PHONY: lint
lint:
	$(GOLANGCI_LINT) run --config .golangci.yml

# Swagger code generation.
.PHONY: swagger-gen
swagger-gen:
	$(SWAG) init -g ./cmd/account-service/app/http_server.go -o docs/account --parseInternal --parseDependency --ot json

# Generate protobuf code.
.PHONY: proto
proto:
	protoc --go_out=. --go-grpc_out=. \
		proto/account/v1/*.proto

# Clean generated protobuf files.
.PHONY: proto-clean
proto-clean:
	find internal/transport/grpc/pb -name "*.pb.go" -delete

# Generate local service artifacts.
.PHONY: generate
generate: swagger-gen proto
