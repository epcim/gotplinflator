
KUSTOMIZE_VERSION = v4.1.2

GO_PLUGIN_NAME="GotplInflator.so"
EXEC_PLUGIN_NAME="GotplInflator"

PLUGIN_REPOSITORY = github.com/epcim/gotplinflator
XDG_CONFIG_HOME ?= $(HOME)/.config
install_dir = $(XDG_CONFIG_HOME)/kustomize/plugin/local/v1/gotplinflator

.PHONY: all test vendor dependencies fixmod build install clean

buid: tidy build-plugin build-exec

install: go-install clean build install-plugin


build-plugin: vendor
	go build -x -trimpath $(GOFLAGS) -buildmode plugin -o ./GotplInflator.so ./GotplInflator.go

install-plugin: tidy build-plugin
	mkdir -p $(install_dir) && cp -v GotplInflator.so $(install_dir)
#.PHONY: install-plugin
#install-plugin:
#	./scripts/install-gotpl.sh

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


.PHONY: goptlinflator
gotplinflator:
	go get -u github.com/epcim/gotplinflator

.PHONY: dependencies
dependencies:
	@go mod download
	@go mod tidy
	@go mod vendor

.PHONY: setup
setup: kustomize gotplinflator dependencies

tidy:
	@go mod tidy

vendor:
	@go mod vendor

test:
	@go test ./GotplInflator_test.go

.PHONY: go-install
go-install:
	go install

clean: clean-plugins
	rm $(GO_PLUGIN_NAME) || true
	rm $(EXEC_PLUGIN_NAME) || true
	@go clean -modcache
	@go clean -cache
	@rm -rf vendor/*

.PHONY: clean-plugins
clean-plugins:
	rm -rf $(XDG_CONFIG_HOME)/kustomize/plugin/local/v1/ || true
	rm -rf $(HOME)/sigs.k8s.io/kustomize/plugin/local/v1/ || true

.PHONY: kustomize
kustomize:
	./scripts/install-kustomize.sh

all: build-plugin install-plugin
