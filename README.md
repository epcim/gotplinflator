
# GotplInflator for Kustomize

See PR as builtin plugin: https://github.com/kubernetes-sigs/kustomize/pull/3490

About:
- This is not replacement for HelmChart generator
- Makes easy reuse, even "pieces" of existing Helm charts (long-term) or other templated manifests available
- To simplify complexity in latter Kustomization

Who might want to use it:
- you have library with existing go-templated manifests already
- you know your manifests and you want low-level control
- deployment perspective
- you live on edge (build from master)
- you store generated templates as versioned artefacts
- you store templates (versioned) linked to your image builds (versioned)


## Status

Work In Progress

Open TODOs are in this [README](https://github.com/epcim/kustomize/blob/gotplinflator/plugin/builtin/gotplinflator/README.md)

Exec plugin might be slightly out-dated. Intention is to use it as builtin plugin anyway.

## Build & Install


Clone, read Makefile and:
```sh
export GO111MODULE=on

go get -u github.com/epcim/gotplinflator
cd $GOPATH/src/github.com/epcim/gotplinflator

make && make install
```

To fix modules to meet your kustomize version:
```sh
# list upstream taged releases
git -c 'versionsort.suffix=-' ls-remote --tags --sort='v:refname' https://github.com/kubernetes-sigs/kustomize 'kustomize/v*.*.*' \
  |tail --lines=10 | cut --delimiter='/' --fields=4

KUSTOMIZE_V=v4.0.1
curl -qsL "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/kustomize/${KUSTOMIZE_V}/kustomize/go.mod" | grep -v "^replace" >| go.mod

go mod tidy
```

## Usage

Example:

```yaml

apiVersion: local/v1
kind: GotplInflator
metadata:
  name: example

dependencies:
- name: nginx
  repo: github.com/epcim/k8s-manifests//example/manifests?ref=main
  #path: example/manifests
  #pull: Always
  #templateGlob:   "*.t*pl"
  #templateOpts    # PLACEHOLDER

values:
  nginx_cpu_request: "512m"
  nginx:
    cpu:
      limit:  "1000m"
    memory:
      limit: "1024M"
```

Optional ENV variables:

```sh
export KUSTOMIZE_DEBUG=true
export KUSTOMIZE_GOTPLINFLATOR_ROOT=$PWD/repos
export KUSTOMIZE_GOTPLINFLATOR_PULL=Always
```

## Caveat

As with many Go plugins, you may have to fork this repo and adjust its go.mod in order to correct package mismatches with your kustomize binary.
