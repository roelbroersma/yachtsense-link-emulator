# Convenience targets for local development and continuous integration.
.PHONY: test build package package-manager package-manager-current source check release clean

test:
	go test ./...

build:
	mkdir -p build/bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 \
		go build -trimpath -ldflags="-s -w" \
		-o build/bin/yachtsense-link-emulator ./cmd/yachtsense-link-emulator

# One firmware-independent RUTX payload for SSH/opkg and all PM wrappers.
package:
	./scripts/build-ipk.sh

# Build one RutOS WebUI wrapper around the generic IPK.
package-manager: package
	@test -n "$(RUTOS_FIRMWARE)" || { \
		echo 'Set RUTOS_FIRMWARE to the target RutOS release.' >&2; \
		echo 'Example: RUTOS_FIRMWARE=RUTX_R_00.07.24.2 make package-manager' >&2; \
		exit 1; \
	}
	./scripts/build-pm-bundle.sh "$(RUTOS_FIRMWARE)"

# Build wrappers for Teltonika's current RUTX Stable and Latest releases.
package-manager-current:
	./scripts/build-current-pm-bundles.sh

source:
	./scripts/source-archive.sh

check:
	./scripts/check.sh

release: check source

clean:
	rm -rf build
	rm -f dist/*.ipk dist/*.sha256 dist/*.tar.gz
