.PHONY: generate schema check test-go test-postgres build-go test-android build-android interop test-host-update distribution test-consumers test-release test-release-gates test-console docs docs-audit verify

generate:
	./scripts/generate.sh

schema:
	./scripts/check-schema.sh

check:
	python3 scripts/check-repository.py

test-go:
	cd go && GOWORK=off go test -race ./...
	cd examples/minimal-server && GOWORK=off go test ./...

test-postgres:
	./scripts/test-postgres.sh

build-go:
	cd go && GOWORK=off go build ./...
	cd examples/minimal-server && GOWORK=off go build -o ../../out/minimal-server .

test-android:
	./android/gradlew -p android :kylok-dpc:testDebugUnitTest

build-android:
	./android/gradlew -p android :kylok-dpc:assembleRelease :minimal-dpc:assembleDebug :kylok-dpc:lintDebug :minimal-dpc:lintDebug :configuration-app:assembleDebug :configuration-app:lintDebug

interop:
	./scripts/test-interop.sh https
	./scripts/test-interop.sh http

test-host-update:
	./scripts/test-host-update.sh

test-console: build-android
	cd examples/minimal-server && KYLOK_CONSOLE_APK="$(CURDIR)/examples/minimal-dpc/build/outputs/apk/debug/minimal-dpc-debug.apk" GOWORK=off go test -race ./...

distribution:
	./android/gradlew -p android :kylok-dpc:publishReleasePublicationToDistributionRepository
	python3 scripts/package-release.py
	python3 scripts/check-distribution.py out/release

test-consumers: distribution
	./scripts/test-consumers.sh

test-release: distribution
	python3 scripts/test-release.py
	python3 scripts/release-assets.py out/release

test-release-gates:
	python3 scripts/test-release.py CheckoutIntegrity AutomaticPublication ReleaseGates

docs-audit:
	python3 scripts/docs.py audit

docs: docs-audit
	./android/gradlew -p android :kylok-dpc:dokkaGeneratePublicationHtml
	.tools/docs-venv/bin/python scripts/docs.py build

verify: generate schema check docs-audit test-go test-postgres build-go test-android build-android interop test-host-update test-console test-consumers test-release
