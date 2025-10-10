.PHONY: all default fmt lint clean build install

default: all

fmt:
	go fmt ./...

lint:
	golint ./...
	go vet ./...

clean:
	go clean -i ./...
	rm -fv ./bin/poster || true

# builds binaries into ./bin/
build:
	mkdir -p bin
	go build -o bin/poster  ./cmd/poster

# installs binaries into $GOBIN
install:
	go install ./cmd/poster

# all
all: fmt lint clean install build

