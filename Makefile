.PHONY: build test vet fmt lint coverage complexity clean

GOFLAGS := -gcflags=all=-l

build:
	go build ./...

test:
	go test $(GOFLAGS) -count=1 ./...

vet:
	go vet ./...

fmt:
	@bad=$$(gofmt -l .); \
	if [ -n "$$bad" ]; then \
		echo "gofmt needed on:"; echo "$$bad"; exit 1; \
	fi

lint: vet fmt

coverage:
	go test $(GOFLAGS) -coverprofile=cover.out ./...
	go tool cover -func=cover.out
	@echo "---"
	@go tool cover -func=cover.out | grep total:

coverage-html: coverage
	go tool cover -html=cover.out -o coverage.html
	open coverage.html

complexity:
	gocyclo -top 20 .

check: lint test complexity

clean:
	rm -f go-tiddle cover.out coverage.html
