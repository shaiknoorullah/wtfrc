.PHONY: build build-monitor test lint clean release install-service e2e e2e-image e2e-shell

build:
	go build -o bin/wtfrc ./cmd/wtfrc
	go build -o bin/wtfrc-monitor ./cmd/wtfrc-monitor

build-monitor:
	go build -o bin/wtfrc-monitor ./cmd/wtfrc-monitor

test:
	go test ./... -v -count=1

lint:
	go vet ./...

clean:
	rm -rf bin/

release:
	goreleaser release --clean

install-service:
	mkdir -p ~/.config/systemd/user
	cp configs/wtfrc-coach.service ~/.config/systemd/user/
	systemctl --user daemon-reload
	@echo "Service installed. Enable with: systemctl --user enable wtfrc-coach"

# --- E2E testing ---

e2e-image:
	@echo "==> Building E2E VM image..."
	bash e2e/scripts/build-image.sh

e2e: build
	@echo "==> Running E2E tests..."
	@if [ -n "$$HYPRLAND_INSTANCE_SIGNATURE" ] && [ -S "$$XDG_RUNTIME_DIR/hypr/$$HYPRLAND_INSTANCE_SIGNATURE/.socket2.sock" ]; then \
		echo "==> Detected local Hyprland, running in local mode"; \
		go test -tags e2e -v -timeout 10m ./e2e/testcases/; \
	else \
		echo "==> No local Hyprland, running in VM mode"; \
		bash e2e/scripts/boot-vm.sh; \
		scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
			-i e2e/.cache/e2e_key -P 2222 \
			bin/wtfrc bin/wtfrc-monitor test@localhost:/usr/local/bin/; \
		go test -tags e2e -v -timeout 10m ./e2e/testcases/; \
		bash e2e/scripts/stop-vm.sh; \
	fi

e2e-shell:
	@echo "==> Booting VM for interactive debugging..."
	@bash e2e/scripts/boot-vm.sh
	ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
		-i e2e/.cache/e2e_key -p 2222 test@localhost
	@bash e2e/scripts/stop-vm.sh
