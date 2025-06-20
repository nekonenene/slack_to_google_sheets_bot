# Default target
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  run     - Run the application"
	@echo "  build   - Build the application"
	@echo "  test    - Run tests"
	@echo "  clean   - Clean build artifacts"
	@echo "  fmt     - Format code"
	@echo "  vet     - Run go vet"
	@echo "  deps    - Download dependencies"

# Run the application
.PHONY: run
run:
	go run main.go

# Build the application
.PHONY: build
build:
	go build -o build/slack-to-google-sheets-bot main.go

# Run tests
.PHONY: test
test:
	go test ./...

# Clean build artifacts
.PHONY: clean
clean:
	rm -rf bin/

# Format code
.PHONY: fmt
fmt:
	go fmt ./...

# Run go vet
.PHONY: vet
vet:
	go vet ./...

# Download dependencies
.PHONY: deps
deps:
	go mod tidy
