
XDG_CONFIG_HOME ?= $(HOME)/.config
install_dir = $(XDG_CONFIG_HOME)/kustomize/plugin/local/v1/gotplinflator
.PHONY: vendor

test:
	go test ./.

vendor:
	go mod vendor

build: vendor build-plugin

install: build install-plugin install-exec

build-plugin: vendor
	go build -trimpath -buildmode plugin -o ./GotplInflator.so ./GotplInflator.go

install-plugin: build-plugin
	mkdir -p $(install_dir) && cp GotplInflator.so $(install_dir)

build-exec: vendor
	go build  -o ./GotplInflator ./exec_plugin.go

install-exec: build-exec
	mkdir -p $(install_dir)-exec && cp GotplInflator $(install_dir)-exec/gotplinflator-exec

clean:
	rm -f GotplInflator.so

all: build-plugin install
