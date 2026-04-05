.PHONY: build test lint clean app run-app

GO ?= go
BIN := audiorec
PKG := ./...

BUNDLE_ID := com.pmoust.audiorec
APP_NAME := audiorec.app
DIST := dist

build:
	$(GO) build -o $(BIN) ./cmd/audiorec

test:
	$(GO) test -race -v $(PKG)

test-short:
	$(GO) test -race -short $(PKG)

lint:
	$(GO) vet $(PKG)

clean:
	rm -rf $(BIN) $(DIST)

# macOS .app bundle for TCC-friendly microphone + screen recording grants.
# Without bundling, each `go build` looks like a new app to macOS and
# permission grants do not persist.
app: build
	@mkdir -p $(DIST)/$(APP_NAME)/Contents/MacOS
	@mkdir -p $(DIST)/$(APP_NAME)/Contents/Resources
	@cp $(BIN) $(DIST)/$(APP_NAME)/Contents/MacOS/$(BIN)
	@printf '%s\n' '<?xml version="1.0" encoding="UTF-8"?>' '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">' '<plist version="1.0">' '<dict>' '  <key>CFBundleIdentifier</key><string>$(BUNDLE_ID)</string>' '  <key>CFBundleName</key><string>audiorec</string>' '  <key>CFBundleDisplayName</key><string>audiorec</string>' '  <key>CFBundleExecutable</key><string>$(BIN)</string>' '  <key>CFBundlePackageType</key><string>APPL</string>' '  <key>CFBundleShortVersionString</key><string>0.1.0</string>' '  <key>CFBundleVersion</key><string>1</string>' '  <key>LSMinimumSystemVersion</key><string>13.0</string>' '  <key>NSMicrophoneUsageDescription</key><string>audiorec records microphone audio for meetings.</string>' '</dict>' '</plist>' > $(DIST)/$(APP_NAME)/Contents/Info.plist
	@codesign --force --sign - $(DIST)/$(APP_NAME)
	@echo "Built $(DIST)/$(APP_NAME)"
	@echo "Run with: $(DIST)/$(APP_NAME)/Contents/MacOS/$(BIN) record -o ./rec"

run-app: app
	$(DIST)/$(APP_NAME)/Contents/MacOS/$(BIN) record -o ./rec
