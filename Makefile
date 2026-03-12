BINARY := naviclaude
PKG := ./cmd/naviclaude
PREFIX ?= $(HOME)/.local

.PHONY: build install clean run popup

build:
	go build -o $(BINARY) $(PKG)

install: build
	@mkdir -p $(PREFIX)/bin
	cp $(BINARY) $(PREFIX)/bin/$(BINARY)
	@echo "Installed to $(PREFIX)/bin/$(BINARY)"

clean:
	rm -f $(BINARY)

run: build
	./$(BINARY)

popup: build install
	tmux display-popup -E -w 85% -h 85% $(PREFIX)/bin/$(BINARY)
