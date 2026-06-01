VENV := .venv
PYTHON := $(VENV)/bin/python
PIP := $(VENV)/bin/pip

.PHONY: setup activate architect debate plan qa run clean-artifacts test-backend install-hooks

# Activate the versioned git hooks (.githooks/pre-commit runs the backend tests
# when backend Go code changes). Run once per clone.
install-hooks:
	git config core.hooksPath .githooks
	@echo "git hooks activados (core.hooksPath=.githooks)"

# Backend Go tests. Go is not installed locally, so we run them inside the same
# golang image used by the backend Dockerfile. The module cache is persisted in
# a named volume (portal2-gomod) so repeated runs are fast.
GO_IMAGE := golang:1.22-alpine
test-backend:
	docker run --rm -e CGO_ENABLED=0 -e GOFLAGS=-mod=mod \
		-v "$(CURDIR)/app/backend":/src \
		-v portal2-gomod:/go/pkg/mod \
		-w /src $(GO_IMAGE) \
		go test ./cmd/server/ -count=1 $(if $(VERBOSE),-v,)

setup:
	python3 -m venv $(VENV)
	$(PIP) install --upgrade pip
	$(PIP) install -r requirements-dev.txt

activate:
	@echo "source $(VENV)/bin/activate"

architect:
	$(PYTHON) spec.py architect

debate:
	$(PYTHON) spec.py debate

plan:
	$(PYTHON) spec.py plan

qa:
	$(PYTHON) spec.py qa

run:
	$(PYTHON) spec.py run

clean-artifacts:
	rm -rf artifacts
