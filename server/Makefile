VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
TARGETS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64
SIGN_IDENTITY ?= Voistic Local
SERVICE := app.voistic.server

.PHONY: build restart test dist clean

# macOS ties firewall and Accessibility approval to the signature, so it's signed
# with the same certificate each time, from scripts/make-signing-cert.sh. An
# ad-hoc signature changes with every build and loses them.
build:
	go build -ldflags "$(LDFLAGS)" -o bin/voistic ./cmd/voistic
ifeq ($(shell uname),Darwin)
	@if security find-certificate -c "$(SIGN_IDENTITY)" >/dev/null 2>&1; then \
		codesign -f -s "$(SIGN_IDENTITY)" -i $(SERVICE) bin/voistic; \
	else \
		echo "no \"$(SIGN_IDENTITY)\" certificate, signing ad hoc"; \
		codesign -f -s - bin/voistic; \
	fi
endif

restart: build
	launchctl kickstart -k gui/$$(id -u)/$(SERVICE)

test:
	go vet ./...
	go test ./...

# One archive per target. Windows gets a second build with no console window, for
# the scheduled task.
dist:
	@rm -rf dist
	@for target in $(TARGETS); do \
		os=$${target%/*}; arch=$${target#*/}; name=voistic-$$os-$$arch; ext=; \
		if [ $$os = windows ]; then ext=.exe; fi; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" \
			-o dist/$$name/voistic$$ext ./cmd/voistic || exit 1; \
		if [ $$os = windows ]; then \
			CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS) -H=windowsgui" \
				-o dist/$$name/voisticw.exe ./cmd/voistic || exit 1; \
			(cd dist && zip -qr $$name.zip $$name) || exit 1; \
			echo dist/$$name.zip; \
		else \
			tar -C dist -czf dist/$$name.tar.gz $$name || exit 1; \
			echo dist/$$name.tar.gz; \
		fi; \
		rm -rf dist/$$name; \
	done

clean:
	rm -rf bin dist
