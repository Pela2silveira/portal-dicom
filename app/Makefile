COMPOSE ?= docker compose
LOG_DIR ?= ./logs
SERVICE ?=
SINCE ?=
TAIL ?= 200

.PHONY: ps logs logs-follow logs-save logs-save-all logs-list

ps:
	$(COMPOSE) ps

logs:
	$(COMPOSE) logs $(if $(SINCE),--since=$(SINCE),) --tail=$(TAIL) $(SERVICE)

logs-follow:
	$(COMPOSE) logs -f $(if $(SINCE),--since=$(SINCE),) --tail=$(TAIL) $(SERVICE)

logs-save:
	@mkdir -p $(LOG_DIR)
	@service_name="$(if $(SERVICE),$(SERVICE),all)"; \
	timestamp="$$(date +%Y%m%d-%H%M%S)"; \
	output="$(LOG_DIR)/$${service_name}-$${timestamp}.log"; \
	$(COMPOSE) logs $(if $(SINCE),--since=$(SINCE),) --tail=$(TAIL) $(SERVICE) | tee "$$output"; \
	echo; \
	echo "saved $$output"

logs-save-all:
	@$(MAKE) logs-save SERVICE=

logs-list:
	@mkdir -p $(LOG_DIR)
	@ls -lt $(LOG_DIR)
