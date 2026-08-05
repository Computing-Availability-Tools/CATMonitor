.PHONY: all build test test-verbose test-coverage lint clean web dfee

GO=go
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
	$(GO) build $(DCMITAG) -o $(BIN) ./cmd/catmonitor

web:
	$(GO) build -o bin/catmonitor-web ./features/web

dfee:
	$(GO) build -o bin/catmonitor-dfee ./features/dfee

test:
	$(GO) test ./...

test-verbose:
	$(GO) test -v ./...

test-coverage:
	$(GO) test -cover ./...

lint:
	$(GO) vet ./...

clean:
	rm -rf bin/

install: build
	cp $(BIN) /usr/local/bin/catmonitor
	mkdir -p /etc/catmonitor
	cp configs/catmonitor.yaml /etc/catmonitor/catmonitor.yaml
