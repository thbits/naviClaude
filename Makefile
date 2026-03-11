BINARY := naviclaude
PKG := ./cmd/naviclaude

.PHONY: build install clean run

build:
	go build -o $(BINARY) $(PKG)

install: build
	cp $(BINARY) $(GOPATH)/bin/$(BINARY) 2>/dev/null || cp $(BINARY) /usr/local/bin/$(BINARY)

clean:
	rm -f $(BINARY)

run: build
	./$(BINARY)
