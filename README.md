
# GotplInflator for Kustomize

CURRENTLY PROTOTYPE ONLY.

This is an alternative for these which workflow is not 100% Kustomize.

Goals:
- reuse existing manifests (in gotpl)
- generic template renderer (gotpl, jinja, jsonent ?)
- by rendering boilerplate, simplify number of vectors and yaml porn in base manifest/repo -> less less source, less patching

## Status

- exec plugin works

## Build & Install

Clone to your Kustomize plugin folder and run:
```sh

make vendor
make build-exec
```

## Usage

Example:

```yaml
apiVersion: github.com/epcim/v1
kind: GotplInflator
metadata:
  name: xyzDependencies

dependencies:
- name: xyz
  #image:
  repo: git@gitlab.com:acme/yyy/xyz//deploy/k8s?ref=master
  #                                 ^- sub-path on repo
  #pull:           IfNotPresent
  #repoCreds:      # PLACEHOLDER
  #templateRegexp: "\\.tm?pl"
  #templateOpts:   # PLACEHOLDER
  #template: gotpl # PLACEHOLDER

values:
  xyz_kms_access: aws
  xyz_cpu_request: "100m"
  xyz:
    cpu:
      limit: 100m
    memory:
      limit: 100M

```

Optional ENV variables:

```sh

export KUSTOMIZE_DEBUG=true
export KUSTOMIZE_GOTPLINFLATOR_PULL=Always
```
