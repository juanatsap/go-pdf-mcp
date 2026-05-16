.PHONY: build install test clean

build:
	go build -o drolo-docs .

install:
	go build -o /usr/local/bin/drolo-docs .

test:
	go test -v -race ./...

clean:
	rm -f drolo-docs
