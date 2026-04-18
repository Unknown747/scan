GO := /nix/store/a90l6nxkqdlqxzgz5j958rz5gwygbamc-go-1.21.13/bin/go
BINARY := eth-scan

.PHONY: build run clean

build:
	$(GO) build -ldflags="-s -w" -o $(BINARY) .
	@echo "Binary: ./$(BINARY)"

run:
	bash run.sh

clean:
	rm -f $(BINARY) found_wallets.txt
