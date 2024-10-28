.DEFAULT_GOAL := all

# seems superfluous
#.PHONY: fmt vet build clean

fmt:
	go fmt ./...


vet: fmt
	go vet ./...

test: build
	go test -v -count=1 ${TESTFLAGS} ./...

bench: build
	go test -bench=.  ./...

build: vet
	mkdir -p bin
	go build -o bin ./cmd/...

clean:
	rm -rf bin


all: build

images:
	docker compose build

helminstall: images
	helm upgrade --install dns helm/dns

push: images
	docker compose --profile build push
