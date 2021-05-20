
KUSTOMIZE_VERSION = v4.1.2
PLUGIN_REPOSITORY = github.com/epcim/gotplinflator
XDG_CONFIG_HOME ?= $(HOME)/.config
install_dir = $(XDG_CONFIG_HOME)/kustomize/plugin/local/v1/gotplinflator
.PHONY: all test vendor dependencies fixmod build install clean

buid: tidy build-plugin build-exec

install: build install-plugin install-exec

build-plugin: vendor
	go build -x -trimpath $(GOFLAGS) -buildmode plugin -o ./GotplInflator.so ./GotplInflator.go

install-plugin: tidy build-plugin
	mkdir -p $(install_dir) && cp -v GotplInflator.so $(install_dir)

build-exec: tidy vendor
	go build -x $(GOFLAGS) -o ./GotplInflator ./exec_plugin.go

install-exec: build-exec
	mkdir -p $(install_dir)-exec && cp  -vGotplInflator $(install_dir)-exec/gotplinflator-exec

fixmod: getmodupstream tidy vendor

getmodupstream:
	@curl -qsL "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/kustomize/$(KUSTOMIZE_VERSION)/kustomize/go.mod" \
		| egrep -v '^replace' \
		| sed 's,^module.*,module $(PLUGIN_REPOSITORY),' >| go.mod
	@curl -qsL "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/kustomize/$(KUSTOMIZE_VERSION)/kustomize/go.sum" >| go.sum

dependencies:
	@go mod download
	@go mod tidy
	@go mod vendor

tidy:
	@go mod tidy

vendor:
	@go mod vendor

test:
	@go test ./GotplInflator_test.go

clean:
	@rm -f GotplInflator.so || true
	@rm -f GotplInflator || true
	@go clean -modcache
	@go clean -cache
	@rm -rf vendor/*

all: build-plugin install-plugin
