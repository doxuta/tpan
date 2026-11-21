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
.PHONY: help web build build-linux release test lint check dev-api dev-web vm-up vm-reset vm-deploy vm-shell m28-nghien-cuu m28-giu-ban e2e-m28 m28-test-vm hoi-quy-m28 clean

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

# T: tên script (không có .sh), A: tham số của script, S: hậu tố file kết quả.
m28-nghien-cuu: ## (M28) Chạy một bản thử ở docs/bang-chung/M28-nghien-cuu trên máy ảo: make m28-nghien-cuu T=<tên>
	bash docs/bang-chung/M28-nghien-cuu/chay.sh '$(T)' '$(A)' '$(S)'

# (M28) Giữ bản build của cây lúc này dưới tên bin/tpan-$(TEN)-linux-<kiến trúc>,
# để e2e chạy đúng bản đó (make e2e-m28 B=0 BIN=bin/tpan-$(TEN)-linux) trong lúc
# cây tiếp tục đổi: bản chưa vá lúc đo trước khi vá (TEN=truoc-ban-va), bản cố
# định lúc chạy bằng chứng cuối (TEN=m28-e2e). bin/ không vào git.
m28-giu-ban: TEN ?= m28-e2e
m28-giu-ban: build-linux ## (M28) Giữ bản build của cây dưới tên khác: make m28-giu-ban TEN=<tên>
	cp bin/tpan-linux-arm64 bin/tpan-$(TEN)-linux-arm64
	cp bin/tpan-linux-amd64 bin/tpan-$(TEN)-linux-amd64

# e2e bước 31 của M28. P: các phần (mặc định chinh). L: file log. B: build hay
# không (E2E_BUILD, mặc định 1). BIN: tiền tố file chạy (E2E_BIN_PREFIX).
e2e-m28: P ?= chinh
e2e-m28: L ?= docs/bang-chung/M28.log
e2e-m28: B ?= 1
e2e-m28: BIN ?= bin/tpan-linux
e2e-m28: ## (M28) Chạy e2e bước 31: make e2e-m28 P="apache nginx" L=docs/bang-chung/M28.log
	@E2E_BUILD=$(B) E2E_BIN_PREFIX=$(BIN) E2E_LOG=$(L) bash scripts/e2e-m28.sh $(P) >$(L) 2>&1; rc=$$?; echo "--- 5 dòng cuối $(L) ---"; tail -n 5 $(L); exit $$rc

# Test cần root của sitefs và system: build go test -c cho Linux đúng kiến trúc
# máy ảo, đẩy vào máy ảo, chạy bằng root. R: biểu thức -test.run (mặc định
# Cua|VungSite|LenhRoot). Gói internal/sitefs có thể chưa tồn tại: không có file
# test thì bỏ qua gói đó (go test -c không sinh file khi gói không có test).
m28-test-vm: R ?= Cua|VungSite|LenhRoot
m28-test-vm: ## (M28) Chạy test cần root của sitefs, system, server trong máy ảo
	@arch="$$(scripts/vm.sh exec dpkg --print-architecture </dev/null | tr -d '[:space:]')"; \
	ran=0; \
	for pkg in sitefs system server; do \
	  bin="bin/m28-$$pkg-test-linux-$$arch"; \
	  rm -f "$$bin"; \
	  mkdir -p bin; \
	  if CGO_ENABLED=0 GOOS=linux GOARCH="$$arch" go test -c -o "$$bin" "./internal/$$pkg" 2>"bin/m28-test-$$pkg.err"; then \
	    if [ -f "$$bin" ]; then \
	      scripts/vm.sh push "$$bin" "/home/ubuntu/e2e-m28/m28-$$pkg-test" </dev/null; \
	      echo "=== internal/$$pkg (-test.run '$(R)') ==="; \
	      TPAN_VM_CAPTURE=1 scripts/vm.sh root bash -c "cd /tmp && /home/ubuntu/e2e-m28/m28-$$pkg-test -test.v -test.run '$(R)'" </dev/null; \
	      ran=1; \
	    else \
	      echo "=== internal/$$pkg: không có file test, bỏ qua ==="; \
	    fi; \
	  else \
	    echo "=== internal/$$pkg: go test -c lỗi ==="; cat "bin/m28-test-$$pkg.err"; \
	  fi; \
	done; \
	[ "$$ran" = 1 ] || { echo "Chưa gói nào chạy được (internal/sitefs có thể chưa tồn tại)"; exit 1; }

# Ba e2e hồi quy, mỗi cái từ máy sạch, không dừng khi một cái lỗi. KHÔNG chạy
# trong lúc thử: mỗi cái mất hàng chục phút (phiên chính chạy).
hoi-quy-m28: ## (M28) Chạy lại e2e M13, M14, M15 làm hồi quy
	-E2E_LOG=docs/bang-chung/M28-hoi-quy-m13.log scripts/e2e-m13.sh chinh >docs/bang-chung/M28-hoi-quy-m13.log 2>&1
	-E2E_LOG=docs/bang-chung/M28-hoi-quy-m14.log scripts/e2e-m14.sh chinh >docs/bang-chung/M28-hoi-quy-m14.log 2>&1
	-E2E_LOG=docs/bang-chung/M28-hoi-quy-m15.log scripts/e2e-m15.sh >docs/bang-chung/M28-hoi-quy-m15.log 2>&1
	@for f in m13 m14 m15; do \
	  l="docs/bang-chung/M28-hoi-quy-$$f.log"; \
	  echo "$$l: $$(grep -c '=> ĐẠT' "$$l" 2>/dev/null || echo 0) đạt, $$(grep -c '=> KHÔNG ĐẠT' "$$l" 2>/dev/null || echo 0) không đạt"; \
	done

clean: ## Xoá kết quả build
	rm -rf bin
	find web/dist -mindepth 1 ! -name .gitkeep -delete
