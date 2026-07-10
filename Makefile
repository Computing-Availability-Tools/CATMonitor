.PHONY: build test test-verbose test-coverage lint clean

GO=go
BIN=bin/catmonitor

build:
	$(GO) build -o $(BIN) ./cmd/catmonitor

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
