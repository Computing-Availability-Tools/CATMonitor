.PHONY: all build test test-verbose test-coverage test-e2e test-stress test-stress-ut \
	test-monitoring-compat \
	test-stress-race test-stress-e2e test-stress-build \
	test-stress-container-e2e \
	test-stress-build-cpu test-stress-build-npu test-stress-deployment \
	test-stress-audit audit-stress-release install-stress-resources lint check format clean web dfee

GO ?= go
BIN=bin/catmonitor

# DCMI (Ascend NPU) collection: auto-detect the CANN DCMI header and add
# -tags dcmi when present, so the daemon picks up NPU DCMI collection on real
# Ascend hosts automatically (web/dfee are read-only consumers and never need it).
# Requires the CANN SDK at link time (header + libdcmi.so) when the tag is on.
# Override:
#   make build DCMITAG=                                 (force off)
#   make build DCMITAG="-tags dcmi"                     (force on)
#   make build DCMI_HDR=/custom/path/dcmi_interface_api.h  (custom header)
DCMI_HDR ?= /usr/local/Ascend/driver/include/dcmi_interface_api.h
DCMITAG  ?= $(if $(wildcard $(DCMI_HDR)),-tags dcmi,)

all: build web dfee

build:
	@echo "build daemon (dcmi: $(if $(DCMITAG),on,off))"
	@mkdir -p bin
	$(GO) build $(DCMITAG) -o $(BIN) ./cmd/catmonitor

web:
	@mkdir -p bin
	$(GO) build -o bin/catmonitor-web ./features/web

dfee:
	@mkdir -p bin
	$(GO) build -o bin/catmonitor-dfee ./features/dfee

test:
	$(GO) test ./...

test-verbose:
	$(GO) test -v ./...

# Coverage report: merged profile (coverage.out) + total + single-file HTML
# report (coverage.html) — same artifacts the CI coverage job produces.
test-coverage:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "HTML report: coverage.html"

# Black-box e2e scenarios (Go, build-tag gated). Compiles the three
# production binaries and drives them through their HTTP/process/file
# boundaries. Requires :19320/:19322/:19323 free. The default `make test`
# does NOT compile these (see tests/e2e/framework/doc.go).
test-e2e:
	$(GO) vet -tags=e2e ./tests/e2e/...
	$(GO) test -tags=e2e ./tests/e2e/... -count=1 -p 1 -v

# Stress has three intentionally separate automated test layers:
# package-local Go unit/component tests, hermetic build/deployment fixtures,
# and a Linux binary-level CLI/Web end-to-end test. Real benchmark performance
# and NPU workload execution remain explicit hardware acceptance gates.
test-stress: test-monitoring-compat test-stress-ut test-stress-build test-stress-e2e

test-monitoring-compat:
	GO_BIN="$(GO)" bash scripts/stress/tests/monitoring_compatibility_test.sh

test-stress-ut:
	$(GO) test ./features/stress/... ./features/web ./internal/config

test-stress-race:
	$(GO) test -race ./features/stress/... ./features/web

test-stress-e2e:
	GO_BIN="$(GO)" bash tests/e2e/stress_workload_plugin_e2e_test.sh

# Requires a running Docker daemon plus prebuilt control/CPU workload images.
# Optional NPU coverage uses the same in-container workload plugin protocol.
test-stress-container-e2e:
	bash tests/e2e/stress_container_e2e_test.sh

test-stress-build: test-stress-build-cpu test-stress-build-npu test-stress-deployment test-stress-audit

test-stress-build-cpu:
	bash scripts/stress/tests/build_cpu_benchmarks_test.sh
	bash scripts/stress/tests/build_cpu_runner_image_test.sh

test-stress-build-npu:
	bash scripts/stress/tests/npu_native_topology_test.sh
	bash scripts/stress/tests/ascend_env_test.sh
	bash scripts/stress/tests/build_npu_burn_image_test.sh
	bash scripts/stress/tests/runtime_preflight_test.sh

test-stress-deployment:
	bash scripts/stress/tests/control_image_build_test.sh
	bash scripts/stress/tests/generate_stress_deployment_test.sh
	bash scripts/stress/tests/container_deployment_test.sh

test-stress-audit:
	bash scripts/stress/tests/audit_stress_release_test.sh

audit-stress-release:
	bash scripts/stress/audit_stress_release.sh

lint:
	$(GO) vet ./...

# Full local gate: gofmt check + go vet + go test (same scope as CI).
# Quick feedback loop before pushing — CI runs this plus e2e and hygiene.
check:
	@unformatted=$$(gofmt -l . 2>/dev/null); \
	if [ -n "$$unformatted" ]; then \
		echo "Files need gofmt (run make format):"; echo "$$unformatted"; exit 1; fi
	$(GO) vet ./...
	$(GO) test ./...

# One-time Go code formatting.
format:
	gofmt -w .

clean:
	rm -rf bin/

install: build
	cp $(BIN) /usr/local/bin/catmonitor
	mkdir -p /etc/catmonitor
	cp configs/catmonitor.yaml /etc/catmonitor/catmonitor.yaml

# Install only reusable deployment resources. Node-specific configuration and
# NPU device mappings are generated explicitly by generate_stress_deployment.sh.
PREFIX ?= /usr/local
install-stress-resources:
	install -d "$(DESTDIR)$(PREFIX)/lib/catmonitor/docker" "$(DESTDIR)$(PREFIX)/lib/catmonitor/scripts/stress"
	install -m 0644 docker/docker-compose.yml docker/docker-compose.config.yml \
		docker/docker-compose.npu.yml docker/docker-compose.stress.yml \
		"$(DESTDIR)$(PREFIX)/lib/catmonitor/docker/"
	install -m 0755 scripts/stress/generate_stress_deployment.sh \
		"$(DESTDIR)$(PREFIX)/lib/catmonitor/scripts/stress/"
