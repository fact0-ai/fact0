.DEFAULT_GOAL := help

VERSION ?=
REMOTE ?= origin

PYTHON_TAG_PREFIX := sdk/python/v
TYPESCRIPT_TAG_PREFIX := sdk/typescript/v
GO_TAG_PREFIX := sdk/go/v

PYTHON_TAG = $(PYTHON_TAG_PREFIX)$(VERSION)
TYPESCRIPT_TAG = $(TYPESCRIPT_TAG_PREFIX)$(VERSION)
GO_TAG = $(GO_TAG_PREFIX)$(VERSION)

define assert_tag_absent
	@tag="$(1)"; \
	if git rev-parse "$$tag" >/dev/null 2>&1; then \
		echo "Tag $$tag already exists locally"; exit 1; \
	fi; \
	if git ls-remote --tags "$(REMOTE)" "refs/tags/$$tag" | grep -q .; then \
		echo "Tag $$tag already exists on $(REMOTE)"; exit 1; \
	fi
endef

.PHONY: help versions check-version
.PHONY: tag-python tag-typescript tag-go tag-all
.PHONY: push-python push-typescript push-go push-all
.PHONY: release-all test-python test-typescript test-go test-all
.PHONY: docs-dev docs-validate

help: ## Show available targets
	@echo "Fact0 SDK release helpers"
	@echo ""
	@echo "Usage:"
	@echo "  make tag-all VERSION=1.0.1       Create local tags for all SDKs"
	@echo "  make push-all VERSION=1.0.1      Push tags to $(REMOTE) (triggers CI publish)"
	@echo "  make release-all VERSION=1.0.1   tag-all + push-all"
	@echo "  make test-all                    Run local SDK test suites"
	@echo ""
	@echo "Individual SDKs:"
	@echo "  make tag-python VERSION=1.0.1"
	@echo "  make push-python VERSION=1.0.1"
	@echo "  (same pattern for typescript and go)"
	@echo ""
	@grep -E '^[a-zA-Z0-9_.-]+:.*##' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*## "}; {printf "  %-18s %s\n", $$1, $$2}'

versions: ## Print package versions from sdk manifests
	@echo "python:     $$(grep '^version = ' sdk/python/pyproject.toml | cut -d'"' -f2)"
	@echo "typescript: $$(node -p "require('./sdk/typescript/package.json').version")"
	@echo "go:         git tag only (see sdk/go/go.mod for module path)"

check-version:
ifndef VERSION
	$(error VERSION is required. Example: make tag-all VERSION=1.0.1)
endif
	@case "$(VERSION)" in \
		*[!0-9.]*) echo "VERSION must be numeric (e.g. 1.0.1)"; exit 1 ;; \
	esac

tag-python: check-version ## Create Python SDK tag (sdk/python/vVERSION)
	$(call assert_tag_absent,$(PYTHON_TAG))
	git tag $(PYTHON_TAG)

tag-typescript: check-version ## Create TypeScript SDK tag (sdk/typescript/vVERSION)
	$(call assert_tag_absent,$(TYPESCRIPT_TAG))
	git tag $(TYPESCRIPT_TAG)

tag-go: check-version ## Create Go SDK tag (sdk/go/vVERSION)
	$(call assert_tag_absent,$(GO_TAG))
	git tag $(GO_TAG)

tag-all: tag-python tag-typescript tag-go ## Create tags for all SDKs

push-python: check-version ## Push Python SDK tag to REMOTE
	git push $(REMOTE) $(PYTHON_TAG)

push-typescript: check-version ## Push TypeScript SDK tag to REMOTE
	git push $(REMOTE) $(TYPESCRIPT_TAG)

push-go: check-version ## Push Go SDK tag to REMOTE
	git push $(REMOTE) $(GO_TAG)

push-all: check-version push-python push-typescript push-go ## Push all SDK tags to REMOTE

release-all: tag-all push-all ## Create and push all SDK tags

test-python: ## Run Python SDK tests
	cd sdk/python && pip install -e '.[dev]' && pytest -q

test-typescript: ## Run TypeScript SDK tests
	cd sdk/typescript && npm ci && npm run build && npm test

test-go: ## Run Go SDK tests
	cd sdk/go && go vet ./... && go test -race -count=1 ./...

test-all: test-python test-typescript test-go ## Run all SDK test suites

docs-dev: ## Run local Mintlify development server for docs
	cd docs && npx mintlify dev

docs-validate: ## Run docs JSON and MDX path validation check
	python3 docs/scripts/validate-docs.py
