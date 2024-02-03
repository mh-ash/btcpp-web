APP_NAME = btcpp-web

.PHONY: dev-run
dev-run:
	trap "pkill $(APP_NAME)" EXIT
	go build -o target/$(APP_NAME) ./cmd/web/main.go
	./target/$(APP_NAME) &
	tailwindcss -i templates/css/input.css -o static/css/mini.css --minify --watch

.PHONY: run
run:
	tailwindcss -i templates/css/input.css -o static/css/mini.css --minify
	go run ./cmd/web/main.go

.PHONY: build
build:
	tailwindcss -i templates/css/input.css -o static/css/mini.css --minify
	go build -o target/$(APP_NAME) ./cmd/web/main.go

.PHONY: all
all: build

.PHONY: clean
clean:
	rm -f target/*
