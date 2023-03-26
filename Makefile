APP_NAME = btcpp-web

.PHONY: run
run:
	go run ./cmd/web/main.go

.PHONY: build
build:
	./tools/tailwind -i templates/css/input.css -o static/css/styles.css --minify
	go build -o target/$(APP_NAME) ./cmd/web/main.go
	cp -a static target/

.PHONY: all
all: build

.PHONY: clean
clean:
	rm -f target/*
