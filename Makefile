BINARY := codex-gateway
LDFLAGS := -s -w
BUILD := CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)"

.PHONY: build build-all clean

build:
	$(BUILD) -o $(BINARY) ./cmd/codex-gateway

build-all: build-darwin-arm64 build-darwin-amd64 build-linux-arm64 build-linux-amd64

build-darwin-arm64:
	GOOS=darwin GOARCH=arm64 $(BUILD) -o $(BINARY)-darwin-arm64 ./cmd/codex-gateway

build-darwin-amd64:
	GOOS=darwin GOARCH=amd64 $(BUILD) -o $(BINARY)-darwin-amd64 ./cmd/codex-gateway

build-linux-arm64:
	GOOS=linux GOARCH=arm64 $(BUILD) -o $(BINARY)-linux-arm64 ./cmd/codex-gateway

build-linux-amd64:
	GOOS=linux GOARCH=amd64 $(BUILD) -o $(BINARY)-linux-amd64 ./cmd/codex-gateway

clean:
	rm -f $(BINARY) $(BINARY)-*
