VERSION ?= dev
LDFLAGS := -s -w
TARGETS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64
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

dist:
	@for target in $(TARGETS); do \
		os=$${target%/*}; arch=$${target#*/}; \
		echo "dist/voistic-$$os-$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" \
			-o dist/voistic-$$os-$$arch ./cmd/voistic || exit 1; \
	done

clean:
	rm -rf bin dist
