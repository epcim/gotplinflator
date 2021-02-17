
KUSTOMIZE_VERSION = v4.0.1
PLUGIN_REPOSITORY = github.com/epcim/gotplinflator
XDG_CONFIG_HOME ?= $(HOME)/.config
install_dir = $(XDG_CONFIG_HOME)/kustomize/plugin/local/v1/gotplinflator
.PHONY: all test vendor dependencies fixmod build install clean

build: tidy build-plugin

install: build install-plugin install-exec

build-plugin:
	go build -trimpath -buildmode plugin -o ./GotplInflator.so ./GotplInflator.go

install-plugin: build-plugin
	mkdir -p $(install_dir) && cp GotplInflator.so $(install_dir)

build-exec: vendor
	go build  -o ./GotplInflator ./exec_plugin.go

install-exec: build-exec
	mkdir -p $(install_dir)-exec && cp GotplInflator $(install_dir)-exec/gotplinflator-exec

fixmod:
	@curl -qsL "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/kustomize/$(KUSTOMIZE_VERSION)/kustomize/go.mod" \
		| egrep -v '^replace' \
		| sed 's,^module.*,module $(PLUGIN_REPOSITORY),' >| go.mod
	@rm -f go.sum
	go mod tidy

dependencies:
	go mod download
	go mod tidy

tidy:
	go mod tidy

vendor:
	go mod vendor

test:
	go test ./GotplInflator_test.go
	#go test ./...

clean:
	@rm -f GotplInflator.so || true
	@rm -f GotplInflator || true
	@go clean -modcache

all: build-plugin install
