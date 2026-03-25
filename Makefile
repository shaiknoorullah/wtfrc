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
	bash e2e/scripts/build-image.sh

e2e:
	GOOS=linux GOARCH=amd64 go build -o bin/wtfrc ./cmd/wtfrc
	GOOS=linux GOARCH=amd64 go build -tags e2e -o bin/wtfrc-agent ./cmd/wtfrc-agent 2>/dev/null || true
	go test -tags e2e -v -timeout 10m ./e2e/testcases/

e2e-shell:
	bash e2e/scripts/boot-vm.sh e2e/.cache && ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i e2e/.cache/e2e_key -p 2222 test@localhost
