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
  do-giao-dien anh-m16 anh-readme anh-xem anh-mau e2e-m16 lay-dev kieu-dang anh-m17 e2e-m17-gd anh-m18 e2e-m18-gd \
  m28-nghien-cuu m28-giu-ban e2e-m28 m28-test-vm hoi-quy-m28 \
  nc-m17 e2e don-tmp thu-m20 e2e-m20 anh-m20

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
	@# Log ghi ra tmp-phien/ (ngoài git) rồi mới chép vào LOG: file log đã commit mà bị ghi đè lúc
	@# bắt đầu chạy thì cây làm việc thành "dirty" và tên bản tpan được đóng gói có đuôi -dirty.
	E2E_LOG=tmp-phien/e2e-m16.log bash scripts/e2e-m16.sh > tmp-phien/e2e-m16.log 2>&1; rc=$$?; cp tmp-phien/e2e-m16.log $(or $(LOG),docs/bang-chung/M16.log); exit $$rc

# ---- M20: hai bước, token API, chứng chỉ panel ----
# Thử của đợt 0 trên máy ảo trong .tpan-vm (từ máy sạch): ACME cho tên panel qua
# vhost mặc định (nginx rồi apache), đồng hồ của máy, CSP. Log vào docs/bang-chung/M20-nghien-cuu/.
M20_THU := docs/bang-chung/M20-nghien-cuu
thu-m20: build-linux ## Thử của đợt 0 M20 trên máy ảo sạch; log vào docs/bang-chung/M20-nghien-cuu/thu-vm.log
	scripts/vm.sh reset
	scripts/vm.sh deploy
	multipass transfer $(M20_THU)/thu-vm.sh "$$(cat .tpan-vm):/tmp/thu-vm.sh" </dev/null
	scripts/vm.sh root bash /tmp/thu-vm.sh > tmp-phien/thu-vm.log 2>&1 </dev/null; rc=$$?; cp tmp-phien/thu-vm.log $(M20_THU)/thu-vm.log; exit $$rc

e2e-m20: ## e2e bước 23 trên máy ảo trong .tpan-vm (hai bước, token, chứng chỉ panel, nâng từ v0.2.0); log vào LOG (mặc định docs/bang-chung/M20.log)
	@# Như e2e-m16: log ghi ra tmp-phien/ rồi mới chép, để tên bản được đóng gói không có đuôi -dirty.
	E2E_LOG=tmp-phien/e2e-m20.log bash scripts/e2e-m20.sh </dev/null >tmp-phien/e2e-m20.log 2>&1; rc=$$?; \
	  cp tmp-phien/e2e-m20.log $(or $(LOG),docs/bang-chung/M20.log); \
	  echo "e2e-m20: mã thoát $$rc, đạt $$(grep -c '=> ĐẠT' tmp-phien/e2e-m20.log), không đạt $$(grep -c '=> KHÔNG ĐẠT' tmp-phien/e2e-m20.log)"; exit $$rc

anh-m20: web ## Chụp các màn của M20 (đăng nhập chặng hai, bật hai bước, Tài khoản, Cài đặt, Công cụ) sáng, tối, 390 px vào docs/bang-chung/M20
	$(ANH) -bo m20 -out docs/bang-chung/M20

anh-m17: web ## Chụp các màn của M17 (Tài nguyên, Gói có lồng, khối lồng của trang site, Thử tấn công, Công cụ) ở 1440 sáng, tối và 390 px vào docs/bang-chung/M17, kiểm như anh-m16
	$(ANH) -bo m17 -out docs/bang-chung/M17

e2e-m17-gd: ## e2e giao diện M17 trên máy ảo trong .tpan-vm (bật lồng ở gói, vào lồng, sửa gói qua bản xem trước, thử tấn công, ra lồng bằng trình duyệt); log vào docs/bang-chung/M17-giao-dien.log
	@# Như e2e-m18-gd: log ghi ra tmp-phien/ rồi mới chép, để tên bản được đóng gói không có đuôi -dirty.
	E2E_LOG=tmp-phien/e2e-m17-gd.log bash scripts/e2e-m17-gd.sh </dev/null >tmp-phien/e2e-m17-gd.log 2>&1; rc=$$?; \
	  cp tmp-phien/e2e-m17-gd.log docs/bang-chung/M17-giao-dien.log; \
	  echo "e2e-m17-gd: mã thoát $$rc, đạt $$(grep -c '=> ĐẠT' tmp-phien/e2e-m17-gd.log), không đạt $$(grep -c '=> KHÔNG ĐẠT' tmp-phien/e2e-m17-gd.log)"; exit $$rc

anh-m18: web ## Chụp các màn của M18 (Backup có đích, cài lên máy mới, Công cụ) ở 1440 sáng, tối và 390 px vào docs/bang-chung/M18, kiểm như anh-m16
	$(ANH) -bo m18 -out docs/bang-chung/M18

e2e-m18-gd: ## e2e giao diện M18 trên máy ảo trong .tpan-vm (đặt đích, khôi phục từ kho bằng trình duyệt); log vào docs/bang-chung/M18-giao-dien.log
	@# Như e2e-m16: log ghi ra tmp-phien/ rồi mới chép, để tên bản được đóng gói không có đuôi -dirty.
	E2E_LOG=tmp-phien/e2e-m18-gd.log bash scripts/e2e-m18-gd.sh </dev/null >tmp-phien/e2e-m18-gd.log 2>&1; rc=$$?; \
	  cp tmp-phien/e2e-m18-gd.log docs/bang-chung/M18-giao-dien.log; \
	  echo "e2e-m18-gd: mã thoát $$rc, đạt $$(grep -c '=> ĐẠT' tmp-phien/e2e-m18-gd.log), không đạt $$(grep -c '=> KHÔNG ĐẠT' tmp-phien/e2e-m18-gd.log)"; exit $$rc

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

# M17: chạy một bản thử nghiên cứu (docs/bang-chung/M17-nghien-cuu/<T>.sh) bằng
# root trong máy ảo của mốc, lưu output cạnh script. TPAN_VM_CAPTURE=1 vì
# `multipass exec` có lúc treo khi output dài (xem đầu scripts/vm.sh).
NC17 := docs/bang-chung/M17-nghien-cuu
nc-m17: ## (M17) Chạy một bản thử nghiên cứu trong máy ảo: make nc-m17 T=t0-hien-trang
	@test -f $(NC17)/$(T).sh || { echo "không có $(NC17)/$(T).sh" >&2; exit 2; }
	@# t0 tự đứng được; các bản thử sau dùng hàm chung trong lib.sh (nối vào trước).
	@{ case "$(T)" in t0-*) ;; *) cat $(NC17)/lib.sh ;; esac; cat $(NC17)/$(T).sh; } | \
	  TPAN_VM_CAPTURE=1 scripts/vm.sh root bash -s > $(NC17)/$(T).ket-qua.txt 2>&1; \
	  rc=$$?; cat $(NC17)/$(T).ket-qua.txt; echo "(mã thoát $$rc; đã lưu $(NC17)/$(T).ket-qua.txt)"

# Xoá một thư mục nháp của thợ trong tmp-phien/ (đã nằm ngoài git). Có target này
# vì lệnh rm gói trong `bash -c` hay `eval` bắt người duyệt tay, làm phiên tự chạy
# đứng chờ. Chỉ nhận tên thư mục nháp bắt đầu bằng dấu gạch dưới, không có / hay ..,
# để không lỡ xoá lời giao việc (tmp-phien/giao-viec) hay thứ gì ngoài tmp-phien.
don-tmp: ## (M17) Xoá một thư mục nháp trong tmp-phien/: make don-tmp T=_nhap
	@case "$(T)" in _[a-z0-9]*) ;; *) echo "T phải là tên thư mục nháp bắt đầu bằng _ (ví dụ _nhap)" >&2; exit 2 ;; esac
	@case "$(T)" in */*|*..*) echo "T không được chứa / hay .." >&2; exit 2 ;; esac
	rm -rf -- "tmp-phien/$(T)"

# Chạy một kịch bản e2e trên máy ảo của mốc và lưu log. Có target này vì phiên tự
# chạy chỉ được gọi `make` và `scripts/vm.sh` (luật 9 của agent-dev/LO-TRINH.md);
# cách gọi giống scripts/hoi-quy.sh (E2E_LOG để script tự soát bí mật trong log).
e2e: ## Chạy một kịch bản e2e, lưu log: make e2e S=e2e-m13.sh P=chinh L=M17-hoi-quy-m13
	@test -f scripts/$(S) || { echo "không có scripts/$(S)" >&2; exit 2; }
	@test -n "$(L)" || { echo "thiếu L=<tên log, không đuôi>" >&2; exit 2; }
	@E2E_LOG=docs/bang-chung/$(L).log bash scripts/$(S) $(P) </dev/null >docs/bang-chung/$(L).log 2>&1; rc=$$?; \
	  echo "$(S) $(P): mã thoát $$rc, đạt $$(grep -c '=> ĐẠT' docs/bang-chung/$(L).log), không đạt $$(grep -c '=> KHÔNG ĐẠT' docs/bang-chung/$(L).log); log: docs/bang-chung/$(L).log"; exit $$rc

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

# e2e đợt 2 (nhóm A) của M28. Cùng biến với e2e-m28.
e2e-m28-dot2: P ?= all
e2e-m28-dot2: L ?= docs/bang-chung/M28-dot2.log
e2e-m28-dot2: B ?= 1
e2e-m28-dot2: BIN ?= bin/tpan-linux
e2e-m28-dot2: ## (M28) Chạy e2e đợt 2: make e2e-m28-dot2 P="nginx apache nang-cap" L=docs/bang-chung/M28-dot2.log
	@E2E_BUILD=$(B) E2E_BIN_PREFIX=$(BIN) E2E_LOG=$(L) bash scripts/e2e-m28-dot2.sh $(P) >$(L) 2>&1; rc=$$?; echo "--- 5 dòng cuối $(L) ---"; tail -n 5 $(L); exit $$rc

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

e2e-m18: ## Chạy e2e bước 21 của M18 trên máy ảo, log vào docs/bang-chung: make e2e-m18 P="chinh" [L=M18]
	E2E_LOG=docs/bang-chung/$(or $(L),M18).log bash scripts/e2e-m18.sh $(P) </dev/null >docs/bang-chung/$(or $(L),M18).log 2>&1; \
	  rc=$$?; echo "e2e-m18 $(P): mã thoát $$rc, đạt $$(grep -c '=> ĐẠT' docs/bang-chung/$(or $(L),M18).log), không đạt $$(grep -c '=> KHÔNG ĐẠT' docs/bang-chung/$(or $(L),M18).log)"; exit $$rc

e2e-hoi-quy: ## Chạy lại e2e của một mốc cũ trên máy ảo, log vào docs/bang-chung: make e2e-hoi-quy M=9 P=all L=M18-hoi-quy-m9
	E2E_LOG=docs/bang-chung/$(L).log bash scripts/e2e-m$(M).sh $(P) </dev/null >docs/bang-chung/$(L).log 2>&1; \
	  rc=$$?; echo "e2e-m$(M) $(P): mã thoát $$rc, đạt $$(grep -c '=> ĐẠT' docs/bang-chung/$(L).log), không đạt $$(grep -c '=> KHÔNG ĐẠT' docs/bang-chung/$(L).log)"; exit $$rc

clean: ## Xoá kết quả build
	rm -rf bin
	find web/dist -mindepth 1 ! -name .gitkeep -delete
