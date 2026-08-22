# Convenience targets for local development and continuous integration.
.PHONY: test build package package-manager source check release clean

test:
	go test ./...

build:
	mkdir -p build/bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 \
		go build -trimpath -ldflags="-s -w" \
		-o build/bin/yachtsense-link-emulator ./cmd/yachtsense-link-emulator

# Build the standalone RutOS IPK for SSH/opkg installation.
package:
	./scripts/build-ipk.sh

# Build the .tar.gz accepted by System -> Package Manager -> Upload.
# RutOS requires an exact firmware match for offline packages.
package-manager:
	@test -n "$(RUTOS_FIRMWARE)" || { \
		echo 'Set RUTOS_FIRMWARE to the exact output of /etc/version.' >&2; \
		echo 'Example: RUTOS_FIRMWARE=RUTX_R_00.07.24.2 make package-manager' >&2; \
		exit 1; \
	}
	RUTOS_FIRMWARE="$(RUTOS_FIRMWARE)" BUILD_PM_BUNDLE=1 ./scripts/build-ipk.sh

source:
	./scripts/source-archive.sh

check:
	./scripts/check.sh

release: check source

clean:
	rm -rf build
	rm -f dist/*.ipk dist/*.sha256 dist/*.tar.gz
