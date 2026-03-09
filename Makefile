BINARY   := bin/memory-palace
BUNDLE   := $(HOME)/Applications/MemoryPalace.app/Contents/MacOS/memory-palace
EMBED_BIN   := bin/mp-embed
SUMMARIZE_BIN := bin/mp-summarize

.PHONY: build swift bundle install reinstall uninstall

build: swift
	go build -o $(BINARY) .

swift: $(EMBED_BIN) $(SUMMARIZE_BIN)

$(EMBED_BIN): cmd/mp-embed/main.swift
	swiftc -O $< -o $@ -framework NaturalLanguage

$(SUMMARIZE_BIN): cmd/mp-summarize/main.swift
	swiftc -O $< -o $@ -framework Foundation

bundle: build
	@mkdir -p $(HOME)/Applications/MemoryPalace.app/Contents/MacOS
	@cp scripts/Info.plist $(HOME)/Applications/MemoryPalace.app/Contents/Info.plist
	@cp $(BINARY) $(BUNDLE)
	@chmod +x $(BUNDLE)
	@codesign --sign "Apple Development: Kashif Shah (RWPECCHSNL)" --force --deep $(HOME)/Applications/MemoryPalace.app 2>/dev/null || \
		codesign --sign - --force --deep $(HOME)/Applications/MemoryPalace.app 2>/dev/null
	@echo "Bundle updated and signed: ~/Applications/MemoryPalace.app"

install:
	@bash scripts/install-memory-palace.sh

reinstall:
	@launchctl bootout "gui/$$(id -u)/com.kashif.memory-palace" 2>/dev/null || true
	@launchctl bootout "gui/$$(id -u)/com.kashif.memory-palace-web" 2>/dev/null || true
	@bash scripts/install-memory-palace.sh

uninstall:
	@launchctl bootout "gui/$$(id -u)/com.kashif.memory-palace" 2>/dev/null || true
	@launchctl bootout "gui/$$(id -u)/com.kashif.memory-palace-web" 2>/dev/null || true
	@rm -f $(HOME)/Library/LaunchAgents/com.kashif.memory-palace.plist
	@rm -f $(HOME)/Library/LaunchAgents/com.kashif.memory-palace-web.plist
	@rm -rf $(HOME)/Applications/MemoryPalace.app
	@echo "Uninstalled."
