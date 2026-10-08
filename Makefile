build:
	go build -o bin/server ./cmd/api

run:
	go run ./cmd/api

vet:
	go vet ./...

clean:
	rm -rf bin server
