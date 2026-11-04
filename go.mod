module github.com/dan4thewin/tibbitwiki

go 1.26.1

require (
	github.com/agiledragon/gomonkey/v2 v2.14.0
	github.com/alecthomas/chroma/v2 v2.23.1
	github.com/arran4/golang-rcs v0.0.18
	github.com/fsnotify/fsnotify v1.9.0
	github.com/hashicorp/golang-lru/v2 v2.0.7
	github.com/yuin/goldmark v1.7.17
	github.com/yuin/goldmark-highlighting/v2 v2.0.0-20230729083705-37449abec8cc
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/dan4thewin/akin-vecsearch v0.1.0
	github.com/dlclark/regexp2 v1.11.5 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/sgrankin/mk v0.0.0-20260226143916-183147f76d6f // indirect
	golang.org/x/sys v0.39.0 // indirect
)

tool github.com/sgrankin/mk

replace github.com/arran4/golang-rcs => github.com/dan4thewin/golang-rcs v0.0.19-0.20261104210209-23fce1f36f0c
