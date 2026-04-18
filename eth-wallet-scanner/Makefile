GO := /nix/store/a90l6nxkqdlqxzgz5j958rz5gwygbamc-go-1.21.13/bin/go
BINARY := eth-scanner

.PHONY: build run-gen run-scan clean

build:
	$(GO) build -o $(BINARY) .
	@echo "Binary: ./$(BINARY)"

run-gen:
	$(GO) run . -gen -start 1 -end 20

run-scan:
	$(GO) run . -start 1 -end 100 -workers 10 -rate 200

clean:
	rm -f $(BINARY) found_wallets.txt

help:
	$(GO) run . -help
