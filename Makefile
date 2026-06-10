BINARY := lympht
INSTALL_DIR := /usr/local/bin

.PHONY: build test install clean

build:
	go build -o $(BINARY) ./cmd/lympht/

test:
	go test ./... -v

install: build
	mv $(BINARY) $(INSTALL_DIR)/$(BINARY)
	@echo "✓ lympht installed to $(INSTALL_DIR)/$(BINARY)"

clean:
	rm -f $(BINARY)
