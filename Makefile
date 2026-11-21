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
.PHONY: help web build build-linux release test test-web lint check dev-api dev-web vm-up vm-reset vm-deploy vm-shell clean \
  do-giao-dien anh-m16 anh-readme anh-xem anh-mau e2e-m16 lay-dev kieu-dang

help: ## Xem danh sách lệnh
	@grep -E '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk -F ':.*## ' '{ printf "  make %-12s %s\n", $$1, $$2 }'

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

# Test logic thuần của giao diện: các file *.test.ts cạnh mã, chạy thẳng bằng
# Node (từ bản 22.18 Node tự bỏ phần kiểu của TypeScript, không cần build). File
# được test chỉ import kiểu (`import type`), nên không cần trình đóng gói.
WEB_TESTS ?= web/src/**/*.test.ts
test-web: ## Chạy test logic thuần của giao diện (node --test)
	node --test "$(WEB_TESTS)"

# Luật kiểu dáng của giao diện bản 2 (web/kieu_dang_test.go) cho riêng vài thư
# mục, kể cả thư mục còn trong danh sách "chưa đổi": thợ đổi xong một màn chạy
# lệnh này cho thư mục của mình. DIR là các tiền tố tính từ web/, cách nhau bằng dấu phẩy.
kieu-dang: ## Kiểm luật kiểu dáng bản 2 cho một màn: make kieu-dang DIR=src/screens/<màn>/,src/i18n/<màn>.ts
	KIEU_DANG_THU="$(DIR)" go test ./web/ -run 'TestKieuDang|TestNhanKhongVietHoa' -count=1

check: lint test test-web ## Chạy hết kiểm tra trước khi commit
	cd web && pnpm typecheck
	bash scripts/install_test.sh

# ---- Giao diện: đo, chụp, e2e của mốc M16 (docs/GIAO-DIEN.md mục 10) ----
# Cổng riêng (1816x) để không đụng script chụp của phiên khác đang chạy trên cùng máy.
ANH := go run ./scripts/anh-readme -listen 127.0.0.1:18161 -debug-port 18162

do-giao-dien: web ## Đo từng màn trên máy demo (dài mấy màn hình, cỡ chữ, chữ mono...); NGUONG=1: sai một ngưỡng cứng là lỗi
	$(ANH) -bo m16-do $(if $(NGUONG),-nguong,)

anh-m16: web ## Chụp mọi màn (sáng, tối, 390 px) vào docs/bang-chung/M16, kiểm lỗi console, bàn phím, tiếng Anh
	$(ANH) -bo m16 -out docs/bang-chung/M16

anh-readme: web ## Chụp lại bảy ảnh của README vào docs/anh (máy demo)
	$(ANH) -bo readme -out docs/anh

anh-xem: web ## Chụp nhanh vài màn của bộ m16 (CHI=cong-cu,nguoi-dung) vào OUT (mặc định tmp-phien/anh, không vào git) để soát giữa đợt
	$(ANH) -bo m16 -out $(or $(OUT),tmp-phien/anh) -chi "$(CHI)"

anh-mau: ## Chụp ba trang mẫu bản 2 vào thư mục OUT (mặc định tmp-phien/mau, không vào git), để đặt cạnh ảnh bản thật
	$(ANH) -bo mau-v2 -out $(or $(OUT),tmp-phien/mau)

e2e-m16: ## e2e bước 19 trên máy ảo trong .tpan-vm (giao diện bản 2: hai vai, tạo site từ giao diện, nâng từ v0.2.0); log vào LOG (mặc định docs/bang-chung/M16.log)
	E2E_LOG=$(or $(LOG),docs/bang-chung/M16.log) bash scripts/e2e-m16.sh > $(or $(LOG),docs/bang-chung/M16.log) 2>&1

lay-dev: ## git fetch origin (trước khi ghép dev vào nhánh mốc)
	git fetch origin

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

clean: ## Xoá kết quả build
	rm -rf bin
	find web/dist -mindepth 1 ! -name .gitkeep -delete
