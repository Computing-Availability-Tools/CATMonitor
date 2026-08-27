.PHONY: all build test test-verbose test-coverage test-stress test-stress-ut \
	test-stress-race test-stress-e2e test-stress-build \
	test-stress-container-e2e \
	test-stress-build-cpu test-stress-build-npu test-stress-deployment \
	test-stress-audit audit-stress-release install-stress-resources lint clean web dfee

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

test-coverage:
	$(GO) test -cover ./...

# Stress has three intentionally separate automated test layers:
# package-local Go unit/component tests, hermetic build/deployment fixtures,
# and a Linux binary-level CLI/Web end-to-end test. Real benchmark performance
# and NPU workload execution remain explicit hardware acceptance gates.
test-stress: test-stress-ut test-stress-build test-stress-e2e

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
	bash scripts/stress/tests/generate_stress_deployment_test.sh
	bash scripts/stress/tests/container_deployment_test.sh

test-stress-audit:
	bash scripts/stress/tests/audit_stress_release_test.sh

audit-stress-release:
	bash scripts/stress/audit_stress_release.sh

lint:
	$(GO) vet ./...

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