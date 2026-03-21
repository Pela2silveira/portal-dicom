VENV := .venv
PYTHON := $(VENV)/bin/python
PIP := $(VENV)/bin/pip

.PHONY: setup activate architect debate plan qa run clean-artifacts

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
