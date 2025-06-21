# Default target
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  run          - Run the application"
	@echo "  build        - Build the application"
	@echo "  build-linux  - Build for Linux deployment"
	@echo "  test         - Run tests"
	@echo "  clean        - Clean build artifacts"
	@echo "  fmt          - Format code"
	@echo "  vet          - Run go vet"
	@echo "  deps         - Download dependencies"
	@echo "  deploy       - Deploy to remote server"
	@echo "  watch-deploy - Watch files and auto-deploy on changes"

.PHONY: init
init:
	go mod tidy
	cp .env.example .env

# Download dependencies
.PHONY: deps
deps:
	go mod tidy

# Run the application
.PHONY: run
run:
	go run main.go

# Build the application
.PHONY: build
build:
	go build -o build/slack-to-google-sheets-bot main.go

# Build for Linux deployment
.PHONY: build-linux
build-linux:
	GOOS=linux GOARCH=amd64 go build -o build/slack-to-google-sheets-bot main.go

# Clean build artifacts
.PHONY: clean
clean:
	rm -rf build/

# Format code
.PHONY: fmt
fmt:
	go fmt ./...

# Run go vet
.PHONY: vet
vet:
	go vet ./...

# Run tests
.PHONY: test
test:
	go test ./...

# Deploy to remote server
.PHONY: deploy
deploy: build-linux
	@if [ ! -f deploy.env ]; then echo "deploy.env not found. Copy from deploy.env.example"; exit 1; fi
	@source deploy.env && rsync -avz --delete build/slack-to-google-sheets-bot $$REMOTE_USER@$$REMOTE_HOST:/home/$$REMOTE_USER/slack-to-google-sheets-bot-dev/
	@source deploy.env && ssh $$REMOTE_USER@$$REMOTE_HOST "sudo systemctl restart slack-to-google-sheets-bot-dev || /home/$$REMOTE_USER/slack-to-google-sheets-bot-dev/slack-to-google-sheets-bot &"

# Watch files and auto-deploy on changes
.PHONY: watch-deploy
watch-deploy:
	@if [ ! -f deploy.env ]; then echo "deploy.env not found. Copy from deploy.env.example"; exit 1; fi
	@source deploy.env && go run scripts/auto-deploy.go $$REMOTE_HOST /home/$$REMOTE_USER/slack-to-google-sheets-bot-dev $$REMOTE_USER
