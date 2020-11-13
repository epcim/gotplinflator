// +build notravis

// Copyright 2019 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

// Disabled on travis, because don't want to install go-getter on travis.

package main_test

import (
	"testing"

	kusttest_test "sigs.k8s.io/kustomize/api/testutils/kusttest"
)

// This test requires having the go-getter binary on the PATH.
func TestGotplInflator(t *testing.T) {
	th := kusttest_test.MakeEnhancedHarness(t).
		PrepExecPlugin("github.com/epcim", "v1", "GotplInflator")
	defer th.Reset()

	m := th.LoadAndRunGenerator(`
apiVersion: github.com/epcim/v1
kind: GotplInflator
metadata:
  name: example
dependencies:
- name: multibases
  #image:
  repo: github.com/kubernetes-sigs/kustomize//examples/multibases?ref=master
  #repoCreds:
  #path: deploy/k8s
  #pull: Always
  #templatePatt:   "*.t*pl"
  #templateOpts    # PLACEHOLDER
  #template: gotpl # PLACEHOLDER

values:
  example_kms_access: aws
  example_cpu_request: "512m"
  example:
	  cpu:
		  limit:  "1000m"
	  memory:
			limit: "1024M"
`)

	th.AssertActualEqualsExpected(m, `
FIXME FIXME FIXME
`)
}
