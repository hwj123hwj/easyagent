.PHONY: build install run chat serve test vet clean tidy help

# ── Binary names ──
BINARY   = easyagent
BRIDGE   = easyagent-bridge
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  = -X main.version=$(VERSION)

# ── Paths ──
GO = go
BIN_DIR = bin

# ── Default ──
help:
	@echo "EasyAgent Build System"
	@echo ""
	@echo "  make build       Build all binaries to bin/"
	@echo "  make install     Install easyagent to $$GOPATH/bin"
	@echo "  make run         Quick run (single prompt)"
	@echo "  make chat        Start interactive TUI chat"
	@echo "  make serve       Start HTTP server"
	@echo "  make test        Run all tests"
	@echo "  make vet         Run go vet"
	@echo "  make clean       Remove bin/ directory"
	@echo "  make tidy        Run go mod tidy"
	@echo ""
	@echo "  make desktop     Build Go native desktop app"
	@echo ""

# ── Build ──
build:
	mkdir -p $(BIN_DIR)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/easyagent
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BRIDGE) ./cmd/easyagent-bridge
	@echo "✅ Built $(BIN_DIR)/$(BINARY) and $(BIN_DIR)/$(BRIDGE)"

build-agent:
	mkdir -p $(BIN_DIR)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/easyagent
	@echo "✅ Built $(BIN_DIR)/$(BINARY)"

# ── Install (puts binary in $GOPATH/bin) ──
install:
	$(GO) install -ldflags "$(LDFLAGS)" ./cmd/easyagent
	@echo "✅ Installed easyagent to $$($(GO) env GOPATH)/bin/"
	@echo "   Run: easyagent"

# ── Run ──
run:
	$(GO) run ./cmd/easyagent --mode run --prompt "$(P)"

chat:
	$(GO) run ./cmd/easyagent --mode chat

serve:
	$(GO) run ./cmd/easyagent --mode serve --listen :8080

# ── Quality ──
test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

clean:
	rm -rf $(BIN_DIR)

tidy:
	$(GO) mod tidy

# ── Native desktop ──
desktop:
	bash scripts/build-native.sh darwin-arm64
