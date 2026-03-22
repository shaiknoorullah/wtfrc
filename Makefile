.PHONY: build build-monitor test lint clean release install-service

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
