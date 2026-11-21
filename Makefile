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
.PHONY: help web build build-linux release test lint check dev-api dev-web vm-up vm-reset vm-deploy vm-shell clean

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

# Thư mục gói phát hành. := để VERSION chỉ tính một lần, khớp với bản đã nhúng
# vào file chạy qua LDFLAGS.
RELEASE_DIR := dist/release/$(VERSION)

release: build-linux ## Đóng gói phát hành vào dist/release/<version>/ (file chạy, install.sh, SHA256SUMS, VERSION)
	rm -rf $(RELEASE_DIR)
	mkdir -p $(RELEASE_DIR)
	cp bin/tpan-linux-amd64 bin/tpan-linux-arm64 scripts/install.sh $(RELEASE_DIR)/
	chmod 0755 $(RELEASE_DIR)/tpan-linux-amd64 $(RELEASE_DIR)/tpan-linux-arm64 $(RELEASE_DIR)/install.sh
	@# Tính sha256 trong chính thư mục gói để dòng chỉ ghi tên file trần:
	@# tpan self-update và install.sh tìm đúng tên tpan-linux-<arch>.
	cd $(RELEASE_DIR) && if command -v sha256sum >/dev/null 2>&1; then \
	  sha256sum tpan-linux-amd64 tpan-linux-arm64 install.sh; \
	else \
	  shasum -a 256 tpan-linux-amd64 tpan-linux-arm64 install.sh; \
	fi > SHA256SUMS
	@# VERSION: tên bản, một dòng. Nút "Kiểm bản mới" của panel (M15) đọc file
	@# này ở nguồn tải để nói được "bản mới nhất là gì" mà không phải tải cả
	@# file chạy. Không nằm trong SHA256SUMS: chỉ để hiện, không được chạy.
	printf '%s\n' '$(VERSION)' > $(RELEASE_DIR)/VERSION
	@echo "Gói phát hành: $(RELEASE_DIR)/"
	@ls -l $(RELEASE_DIR)

test: ## Chạy unit test
	go test ./...

lint: ## go vet + golangci-lint
	go vet ./...
	golangci-lint run

check: lint test ## Chạy hết kiểm tra trước khi commit
	cd web && pnpm typecheck
	bash scripts/install_test.sh

dev-api: ## Chạy backend chế độ demo (máy giả) ở 127.0.0.1:18080; mở thêm một terminal cho dev-web
	go run ./cmd/tpan serve --demo --listen 127.0.0.1:18080

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

e2e-m18: ## Chạy e2e bước 21 của M18 trên máy ảo, log vào docs/bang-chung: make e2e-m18 P="chinh" [L=M18]
	E2E_LOG=docs/bang-chung/$(or $(L),M18).log bash scripts/e2e-m18.sh $(P) </dev/null >docs/bang-chung/$(or $(L),M18).log 2>&1; \
	  rc=$$?; echo "e2e-m18 $(P): mã thoát $$rc, đạt $$(grep -c '=> ĐẠT' docs/bang-chung/$(or $(L),M18).log), không đạt $$(grep -c '=> KHÔNG ĐẠT' docs/bang-chung/$(or $(L),M18).log)"; exit $$rc

e2e-hoi-quy: ## Chạy lại e2e của một mốc cũ trên máy ảo, log vào docs/bang-chung: make e2e-hoi-quy M=9 P=all L=M18-hoi-quy-m9
	E2E_LOG=docs/bang-chung/$(L).log bash scripts/e2e-m$(M).sh $(P) </dev/null >docs/bang-chung/$(L).log 2>&1; \
	  rc=$$?; echo "e2e-m$(M) $(P): mã thoát $$rc, đạt $$(grep -c '=> ĐẠT' docs/bang-chung/$(L).log), không đạt $$(grep -c '=> KHÔNG ĐẠT' docs/bang-chung/$(L).log)"; exit $$rc

m18-nc: ## (M18, đợt 0) chạy một bản thử nghiên cứu trên Mac: make m18-nc S=02-thu-vien-s3
	bash docs/bang-chung/M18-nghien-cuu/$(S).sh

clean: ## Xoá kết quả build
	rm -rf bin
	find web/dist -mindepth 1 ! -name .gitkeep -delete
