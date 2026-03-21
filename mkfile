GOFLAGS=-gcflags=all=-l

build:V:
	go build ./...

test:V:
	go test $GOFLAGS -count=1 ./...

vet:V:
	go vet ./...

fmt:V:
	bad=`gofmt -l .`
	if [ -n "$bad" ]; then
		echo 'gofmt needed on:'
		echo "$bad"
		exit 1
	fi

lint:V: vet fmt

coverage:V:
	go test $GOFLAGS -coverprofile=cover.out ./...
	go tool cover -func=cover.out
	echo '---'
	go tool cover -func=cover.out | grep total:

coverage-html:V: coverage
	go tool cover -html=cover.out -o coverage.html
	open coverage.html

complexity:V:
	gocyclo -top 20 .

check:V: lint test complexity

clean:V:
	rm -f go-tiddle cover.out coverage.html

install-hooks:V:
	ln -sf ../../scripts/pre-commit .git/hooks/pre-commit
	echo 'pre-commit hook installed'
