SHERPA_VERSION ?= v1.13.8
SHERPA_HOME ?= $(CURDIR)/vendor/sherpa-onnx
GO ?= go
BIN := bin/voxpi

export CGO_ENABLED := 1
export CGO_CFLAGS := -I$(SHERPA_HOME)/include -O2
export CGO_LDFLAGS := -L$(SHERPA_HOME)/lib -lsherpa-onnx-c-api -lonnxruntime \
	-Wl,-rpath,'$$ORIGIN/lib' -Wl,-rpath,$(SHERPA_HOME)/lib

.PHONY: all build vet test fmt run clean libs models

all: build

build:
	@test -f $(SHERPA_HOME)/lib/libsherpa-onnx-c-api.so || \
		{ echo "缺少 sherpa-onnx 库，先执行: make libs"; exit 1; }
	$(GO) build -trimpath -ldflags "-s -w" -o $(BIN) ./cmd/voxpi
	@echo "built $(BIN)"

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

fmt:
	$(GO) fmt ./...

run: build
	./$(BIN) -config voxpi.json

libs:
	./scripts/fetch-sherpa.sh $(SHERPA_VERSION) $(SHERPA_HOME)

models:
	./scripts/fetch-models.sh

clean:
	rm -rf bin
