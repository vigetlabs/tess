BIN := bin/tess
SOURCES := $(shell find . -type f -name '*.go' -not -path './bin/*' -not -path './vendor/*')

.PHONY: all build clean

all: build

build: $(BIN)

$(BIN): $(SOURCES)
	@mkdir -p bin
	go build -o $(BIN) ./cmd

clean:
	rm -rf bin

