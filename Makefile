# Các lệnh làm việc hằng ngày. Gõ `make` để xem danh sách.
MODULE  := github.com/doxuta/tpan
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
  -X $(MODULE)/internal/buildinfo.Version=$(VERSION) \
  -X $(MODULE)/internal/buildinfo.Commit=$(COMMIT) \
  -X $(MODULE)/internal/buildinfo.Date=$(DATE)

.DEFAULT_GOAL := help
.PHONY: help web build build-linux test lint check dev-api dev-web vm-up vm-reset vm-deploy vm-shell clean

help: ## Xem danh sách lệnh
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F ':.*## ' '{ printf "  make %-12s %s\n", $$1, $$2 }'

web: ## Build giao diện vào web/dist
	cd web && pnpm install --frozen-lockfile && pnpm build
	@touch web/dist/.gitkeep

build: web ## Build tpan cho máy đang dùng (bin/tpan)
	go build -ldflags '$(LDFLAGS)' -o bin/tpan ./cmd/tpan

build-linux: web ## Build tpan cho Linux amd64 (VPS) và arm64 (máy ảo trên Mac M)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/tpan-linux-amd64 ./cmd/tpan
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/tpan-linux-arm64 ./cmd/tpan

test: ## Chạy unit test
	go test ./...

lint: ## go vet + golangci-lint
	go vet ./...
	golangci-lint run

check: lint test ## Chạy hết kiểm tra trước khi commit
	cd web && pnpm typecheck

dev-api: ## Chạy backend ở 127.0.0.1:8080 (mở thêm một terminal cho dev-web)
	go run ./cmd/tpan serve --listen 127.0.0.1:8080

dev-web: ## Chạy giao diện ở localhost:5173, tự nạp lại khi sửa code
	cd web && pnpm dev

vm-up: ## Tạo máy ảo test Ubuntu 24.04 và snapshot "clean"
	scripts/vm.sh up

vm-reset: ## Đưa máy ảo test về trạng thái sạch
	scripts/vm.sh reset

vm-deploy: build-linux ## Build rồi chép tpan vào máy ảo test
	scripts/vm.sh deploy

vm-shell: ## Mở shell trong máy ảo test
	scripts/vm.sh shell

clean: ## Xoá kết quả build
	rm -rf bin
	find web/dist -mindepth 1 ! -name .gitkeep -delete
