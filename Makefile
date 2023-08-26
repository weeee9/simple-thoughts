ROJECT="md2html"

SOURCE ?= $(shell find . -type f -name '*.go' -not -path '*/generated/*')

all: build

build:
	go build -v -o bin/md2html .

test:
	@echo $(SOURCE)
	go test -v -tags="json1" ./...
	@echo "===\033[32m OK \033[0m==="