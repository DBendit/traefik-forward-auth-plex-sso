
format:
	gofmt -w -s internal/*.go cmd/*.go

test:
	go test -v ./...

.PHONY: format test

