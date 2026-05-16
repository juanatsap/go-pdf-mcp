.PHONY: build install test clean

build:
	go build -o drolo-mcp-docs .

install:
	go build -o /usr/local/bin/drolo-mcp-docs .

test:
	go test -v -race ./...

clean:
	rm -f drolo-mcp-docs
