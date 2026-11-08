.DEFAULT_GOAL := help
ARGS := $(wordlist 2,$(words $(MAKECMDGOALS)),$(MAKECMDGOALS))
$(eval $(ARGS):;@:)

.PHONY: help test test-race test-integration test-coverage vet tidy modules-check generate check-generated release-plan release-check bench

help: ## Show available commands.
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: ## Run every module's unit and executable example tests.
	@scripts/test-unit.sh

test-race: ## Run race tests across every module.
	@scripts/test-all-modules.sh race

test-integration: ## Run real drivers: make test-integration [all|bleve|meilisearch|typesense|opensearch|elasticsearch|postgres|sqlite|redis].
	@scripts/test-integration.sh $(or $(firstword $(ARGS)),all)

test-coverage: ## Run unit and real-backend tests and generate merged coverage.
	@scripts/coverage.sh

vet: ## Vet every module, including integration fixtures.
	@scripts/test-all-modules.sh vet

tidy: ## Tidy every module against the unpublished local root.
	@scripts/tidy-modules.sh

modules-check: ## Verify module inventory and published dependency boundaries.
	@scripts/check-module-manifest.sh

generate: ## Regenerate the API index, example snippets, and evidence badges.
	@GOWORK=off go -C docs run ./readme

check-generated: ## Verify two generations leave documentation and examples unchanged.
	@scripts/check-generated.sh

release-plan: ## Plan module tags: make release-plan v0.1.0.
	@scripts/tag-all-modules.sh $(firstword $(ARGS)) --dry-run

release-check: ## Validate a published release: make release-check v0.1.0.
	@scripts/check-public-release.sh $(firstword $(ARGS))

bench: ## Benchmark validation and the embedded driver over a fixed corpus.
	@GOWORK=off go test -run '^$$' -bench . -benchmem
	@GOWORK=off go -C examples test -run '^$$' -bench . -benchmem
