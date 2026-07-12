.PHONY: build build-linux build-windows build-darwin clean deps check-opus

BIN_DIR := bin

build: check-opus
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=1 go build -o $(BIN_DIR)/streamd ./cmd/streamd
	CGO_ENABLED=1 go build -o $(BIN_DIR)/streamctl ./cmd/streamctl
	@echo "Built $(BIN_DIR)/streamd and $(BIN_DIR)/streamctl"

build-linux: check-opus
	GOOS=linux GOARCH=amd64 CGO_ENABLED=1 go build -o $(BIN_DIR)/streamd-linux-amd64 ./cmd/streamd
	GOOS=linux GOARCH=amd64 CGO_ENABLED=1 go build -o $(BIN_DIR)/streamctl-linux-amd64 ./cmd/streamctl

build-windows: check-opus
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 go build -o $(BIN_DIR)/streamd.exe ./cmd/streamd
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 go build -o $(BIN_DIR)/streamctl.exe ./cmd/streamctl

build-darwin-arm64: check-opus
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go build -o $(BIN_DIR)/streamd-darwin-arm64 ./cmd/streamd
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go build -o $(BIN_DIR)/streamctl-darwin-arm64 ./cmd/streamctl

build-darwin-amd64: check-opus
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 go build -o $(BIN_DIR)/streamd-darwin-amd64 ./cmd/streamd
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 go build -o $(BIN_DIR)/streamctl-darwin-amd64 ./cmd/streamctl

deps:
	go mod download
	go mod tidy

check-opus:
	@pkg-config --exists opus || (echo "ERROR: libopus not found. Install it first:" && \
	 echo "  Ubuntu/Debian: sudo apt install libopus-dev pkg-config" && \
	 echo "  Fedora:        sudo dnf install opus-devel pkgconfig" && \
	 echo "  macOS:         brew install opus pkg-config" && \
	 echo "  Windows:       install via MSYS2: pacman -S mingw-w64-x86_64-opus pkg-config" && exit 1)

clean:
	rm -rf $(BIN_DIR)
