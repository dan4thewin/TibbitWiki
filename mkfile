GOFLAGS=-gcflags=all=-l

all:V: tibbitwiki

GOFILES=main.go internal/config/config.go internal/config/defaults.yaml internal/editor/editor.go internal/loader/loader.go internal/rcs/rcs.go internal/render/render.go internal/search/search.go internal/server/server.go internal/server/index.html internal/store/store.go internal/tid/tid.go internal/watcher/watcher.go internal/wiki/hub.go internal/wiki/wiki.go internal/wiki/atime_darwin.go internal/wiki/atime_linux.go

tibbitwiki: $GOFILES
	go build -o $target .

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
	rm -f tibbitwiki cover.out coverage.html

install-hooks:V:
	ln -sf ../../scripts/pre-commit .git/hooks/pre-commit
	echo 'pre-commit hook installed'
