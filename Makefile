# Project variables
BINARY_NAME=g-man-bot
PKG=$(shell go list ./... | grep -v /vendor/)
COVER_OUT?=coverage.out
COVER_PKG?=$(PKG)

# Path to the Steam API JSON schema (download it manually via ISteamWebAPIUtil/GetSupportedAPIList)
API_JSON=api.steampowered.com.json
GEN_OUT=pkg/steam/webapi/generated.go

# Colors for console output
CYAN  := \033[0;36m
RESET := \033[0m

.PHONY: all build test race cover cover-clean lint generate clean format help

all: generate race build ## Run the full cycle: generation, tests and assembly

test: ## Run normal quick tests
	@printf "$(CYAN)Running unit tests...$(RESET)\n"
	go test -v $(PKG)

race: ## Run tests with race detector
	@printf "$(CYAN)Running tests with race detector...$(RESET)\n"
	go test -v -race -timeout 30s $(PKG)

cover: ## Run tests and open the coverage report in a browser
	@printf "$(CYAN)Generating coverage report...$(RESET)\n"
	go test -coverprofile=$(COVER_OUT) $(COVER_PKG)
	go tool cover -html=$(COVER_OUT)

cover-clean: ## Display the clean coverage report in the terminal
	@printf "$(CYAN)Generating clean coverage report...$(RESET)\n"
	go test -coverprofile=$(COVER_OUT) $(COVER_PKG)
	go run cmd/coverage/main.go --file=$(COVER_OUT) --sort=percent

generate: ## Update all generated files (manual review required)
	cd cmd/generator && go run main.go webapi proto steamlang format

build: ## Build the bot executable file
	@printf "$(CYAN)Building bot binary...$(RESET)\n"
	go build -o bin/$(BINARY_NAME) cmd/bot/main.go

lint: ## Check the code with a linter (requires golangci-lint)
	@printf "$(CYAN)Running linter...$(RESET)\n"
	golangci-lint run ./...

clean: ## Delete temporary files and binaries
	@printf "$(CYAN)Cleaning up...$(RESET)\n"
	rm -rf bin/
	rm -f coverage.out

format: ## Run go code formatting
	cd cmd/generator && go run main.go format
	addlicense -c "Lemon4ksan" -l bsd -ignore "cmd/generator/protobufs/**" -ignore "**/*.yml" .
	golangci-lint run --fix

help: ## Show this message
	@printf "Usage: make [target]\n"
	@printf "\n"
	@printf "Targets:\n"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
