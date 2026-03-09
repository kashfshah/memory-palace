BINARY   := bin/memory-palace
BUNDLE   := $(HOME)/Applications/MemoryPalace.app/Contents/MacOS/memory-palace

.PHONY: build bundle install reinstall uninstall

build:
	go build -o $(BINARY) .

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
