
.PHONY: vendor

test:
	go test ./. #gotplinflator_test.go

vendor:
	go mod vendor

build: vendor
	go build -buildmode plugin -o ./GotplInflator.so ./GotplInflator.go

build-exec: vendor
	go build  -o ./GotplInflator ./exec_plugin.go
