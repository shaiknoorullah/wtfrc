.PHONY: build test lint clean

build:
	go build -o bin/wtfrc ./cmd/wtfrc

test:
	go test ./... -v -count=1

lint:
	go vet ./...

clean:
	rm -rf bin/
