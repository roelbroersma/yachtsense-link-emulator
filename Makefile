# Convenience targets for local development and continuous integration.
.PHONY: test build package source check release clean

test:
	go test ./...

build:
	mkdir -p build/bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 \
		go build -trimpath -ldflags="-s -w" \
		-o build/bin/yachtsense-link-emulator ./cmd/yachtsense-link-emulator

package:
	./scripts/build-ipk.sh

source:
	./scripts/source-archive.sh

check:
	./scripts/check.sh

release: check source

clean:
	rm -rf build
	rm -f dist/*.ipk dist/*.sha256 dist/*.tar.gz
