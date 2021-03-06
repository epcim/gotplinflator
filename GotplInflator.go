// Copyright 2019 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

//go:generate pluginator
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig"
	getter "github.com/hashicorp/go-getter"
	"sigs.k8s.io/kustomize/api/filesys"
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/api/types"

	"sigs.k8s.io/yaml"

	yamlv2 "gopkg.in/yaml.v2"
)

// RemoteResource is generic specification for remote resources (git, s3, http...)
type RemoteResource struct {
	// local name for remote
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	// go-getter compatible uri to remote
	Repo string `json:"repo" yaml:"repo"`
	// go-getter creds profile for private repos, s3, etc..
	RepoCreds string `json:"repoCreds" yaml:"repoCreds"`
	// PLACEHOLDER, subPath at repo
	Path string `json:"path,omitempty" yaml:"path,omitempty"`
	// pull policy
	Pull string `json:"pull,omitempty" yaml:"pull,omitempty"`
	// template
	Template        string `json:"template,omitempty" yaml:"template,omitempty"`
	TemplatePattern string `json:"templatePattern,omitempty" yaml:"templatePattern,omitempty"`
	TemplateOpts    string `json:"templateOpts,omitempty" yaml:"templateOpts,omitempty"`
	// kinds
	Kinds []string `json:"kinds,omitempty" yaml:"kinds,omitempty"`

	// Dir is where the resource is cloned
	Dir string
}

// GotplInflatorArgs metadata to fetch and render remote templates
type GotplInflatorArgs struct {
	// local name for remote
	Name         string                 `json:"name,omitempty" yaml:"name,omitempty"`
	Dependencies []RemoteResource       `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	Values       map[string]interface{} `json:"values,omitempty" yaml:"values,omitempty"`
}

var KustomizePlugin GotplInflatorPlugin

var gotplFilePattern = "*.t*pl"
var manifestFilePattern = "*.y*ml"
var renderedManifestFilePattern = "*.rendered.y*ml"
var SprigCustomFuncs = map[string]interface{}{
	"handleEnvVars": func(rawEnvs interface{}) map[string]string {
		envs := map[string]string{}
		if str, ok := rawEnvs.(string); ok {
			err := json.Unmarshal([]byte(str), &envs)
			if err != nil {
				log.Fatal("failed to unmarshal Envs,", err)
			}
		}
		return envs
	},
	//
	// Shameless copy from:
	// https://github.com/helm/helm/blob/master/pkg/engine/engine.go#L107
	//
	// Some more Helm template functions:
	// https://github.com/helm/helm/blob/master/pkg/engine/funcs.go
	//
	"toYaml": func(v interface{}) string {
		data, err := yaml.Marshal(v)
		if err != nil {
			// Swallow errors inside of a template.
			return ""
		}
		return strings.TrimSuffix(string(data), "\n")
	},
}

// GotplInflatorPlugin is a plugin to generate resources
// from a remote or local go templates.
type GotplInflatorPlugin struct {
	//type remoteResource struct {
	h                 *resmap.PluginHelpers
	types.ObjectMeta  `json:"metadata,omitempty" yaml:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	GotplInflatorArgs //types.GotplInflatorArgs

	rf *resmap.Factory

	TempDir string

	GotplInflatorRoot string
}

// Config uses the input plugin configurations `config` to setup the generator
//func (p *plugin) Config(
func (p *GotplInflatorPlugin) Config(h *resmap.PluginHelpers, config []byte) error {

	p.h = h
	err := yaml.Unmarshal(config, p)
	if err != nil {
		return err
	}
	return nil
}

// GotplRender process templates
func (p *GotplInflatorPlugin) GotplRender(t string) error {

	// read template
	tContent, err := ioutil.ReadFile(t)
	if err != nil {
		return fmt.Errorf("Read template failed: %v", err)
	}

	// init
	fMap := sprig.TxtFuncMap()
	for k, v := range SprigCustomFuncs {
		fMap[k] = v
	}
	//tOpt := strings.Split(rs.TemplateOpts, ",")
	tpl := template.Must(
		//.Option(tOpt)
		//.ParseGlob("*.html")
		template.New(t).Funcs(fMap).Parse(string(tContent)),
	)

	//render
	var rb bytes.Buffer
	err = tpl.Execute(&rb, p.Values)
	if err != nil {
		return err
	}

	// write
	tBasename := strings.TrimSuffix(t, filepath.Ext(t))
	tBasename = strings.TrimSuffix(t, filepath.Ext(tBasename)) // removes .yaml
	err = ioutil.WriteFile(tBasename+".rendered.yaml", rb.Bytes(), 0640)
	if err != nil {
		//log.Fatal("Write template failed:", err)
		return fmt.Errorf("Write template failed: %v", err)
	}
	return nil
}

// Generate fetch, render and return manifests from remote sources
func (p *GotplInflatorPlugin) Generate() (resmap.ResMap, error) {

	//DEBUG
	//for _, e := range os.Environ() {
	//    pair := strings.SplitN(e, "=", 2)
	//	fmt.Printf("#DEBUG %s='%s'\n", pair[0], pair[1])
	//}

	// FIXME - hardcoded /envs/ will go away and will be replaced by config option
	var pluginConfigRoot = os.Getenv("KUSTOMIZE_PLUGIN_CONFIG_ROOT")
	if os.Getenv("KUSTOMIZE_GOTPLINFLATOR_ROOT") == "" {
		var envsPath = strings.SplitAfter(pluginConfigRoot, "/envs/")
		if len(envsPath) > 1 {
			os.Setenv("KUSTOMIZE_GOTPLINFLATOR_ROOT", filepath.Join(envsPath[0], "../repos"))
			os.Setenv("ENV", strings.Split(envsPath[1], "/")[0])
		}
	}

	// where to fetch, render, otherwise tempdir
	p.GotplInflatorRoot = os.Getenv("KUSTOMIZE_GOTPLINFLATOR_ROOT")

	// tempdir
	err := p.getTempDir()
	if err != nil {
		return nil, fmt.Errorf("Failed to create temp dir: %s", p.TempDir)
	}

	// normalize context values used for template rendering
	nv := make(map[string]interface{})
	FlattenMap("", p.Values, nv)
	p.Values = nv
	//DEBUG
	//for k, v := range p.Values {
	//	fmt.Printf("%s:%s\n", k, v)
	//}

	// fetch dependencies
	err = p.fetchDependencies()
	if err != nil {
		return nil, fmt.Errorf("Error getting remote source: %v", err)
	}

	// render to files
	err = p.RenderDependencies()
	if err != nil {
		return nil, fmt.Errorf("Template rendering failed: %v", err)
	}

	// prepare output buffer
	var output bytes.Buffer
	output.WriteString("\n---\n")

	// collect, filter, parse manifests
	err = p.ReadManifests(&output)
	if err != nil {
		return nil, fmt.Errorf("Read manifest failed: %v", err)
	}

	// cleanup
	var cleanupOpt = os.Getenv("KUSTOMIZE_GOTPLINFLATOR_CLEANUP")
	if p.GotplInflatorRoot != "" && cleanupOpt == "ALWAYS" {
		err = p.CleanWorkdir()
		if err != nil {
			return nil, fmt.Errorf("Cleanup failed: %v", err)
		}
	}

	return p.h.ResmapFactory().NewResMapFromBytes(output.Bytes())
}

// getTempDir prepare working directory
func (p *GotplInflatorPlugin) getTempDir() error {
	if p.GotplInflatorRoot != "" {
		//p.TempDir = filepath.Join(p.GotplInflatorRoot, os.Getenv("ENV"))
		p.TempDir = filepath.Join(p.GotplInflatorRoot)
		if _, err := os.Stat(p.TempDir); os.IsNotExist(err) {
			err := os.MkdirAll(p.TempDir, 0770)
			if err != nil {
				return err
			}
		}
	} else {
		var _tempDir filesys.ConfirmedDir
		_tempDir, err := filesys.NewTmpConfirmedDir()
		p.TempDir = _tempDir.String()
		if err != nil {
			return err
		}
	}
	// DEBUG
	// fmt.Println("# TempDir:", p.TempDir)
	return nil
}

// RenderDependencies render gotpl manifests
func (p *GotplInflatorPlugin) RenderDependencies() error {
	for _, rs := range p.Dependencies {

		//fmt.Println("# Rendering:", rs.Name)

		// TODO, render manifests to output buffer directly. So it does not require

		// find templates
		if rs.TemplatePattern != "" {
			gotplFilePattern = rs.TemplatePattern
		}
		templates, err := WalkMatch(rs.Dir, gotplFilePattern)
		if os.IsNotExist(err) {
			//fmt.Println("# - no templates found")
			continue
		} else if err != nil {
			return err
		}

		// actual render
		for _, t := range templates {
			//fmt.Printf("# - %s\n", strings.SplitAfter(t, p.TempDir)[1])
			err := p.GotplRender(t)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// fetchDependencies calls go-getter to fetch remote sources
func (p *GotplInflatorPlugin) fetchDependencies() error {
	for idx, rs := range p.Dependencies {

		// where to fetch remote resource (ie: /TempDir/reponame-branch etc..)
		var err error
		rs.Dir, err = p.setFetchDst(idx)
		if err != nil {
			return fmt.Errorf("Failed to process donwload destination for resource %s.", rs.Name)
		}

		// skip fetch if is present and not forced
		_, err = os.Stat(rs.Dir)
		if err == nil && strings.ToLower(rs.Pull) != "always" && strings.ToLower(os.Getenv("KUSTOMIZE_GOTPLINFLATOR_PULL")) != "always" {
			continue
		}

		// cleanup,create dest dir
		if os.IsNotExist(err) {
			_ = os.MkdirAll(rs.Dir, 0770)
		} else {
			err := os.RemoveAll(rs.Dir)
			if err != nil {
				return err
			}
		}

		//fetch
		pwd, err := os.Getwd()
		if err != nil {
			return err
		}
		opts := []getter.ClientOption{}

		//Chan, other options, ...
		//https://github.com/hashicorp/go-getter/blob/main/cmd/go-getter/main.go

		//Handle credentials
		//https://github.com/hashicorp/go-getter#git-git
		gettercreds, err := getRepoCreds(rs.RepoCreds)
		if err != nil {
			return err
		}

		client := &getter.Client{
			Ctx:     context.TODO(),
			Src:     rs.Repo + gettercreds,
			Dst:     rs.Dir,
			Pwd:     pwd,
			Mode:    getter.ClientModeAny,
			Options: opts,
		}

		// PLACEHOLDER detectors/getters
		//httpGetter := &getter.HttpGetter{
		//	Netrc: true,
		//}
		//	Detectors: []getter.Detector{
		//		new(getter.GitHubDetector),
		//		new(getter.GitLabDetector),
		//		new(getter.GitDetector),
		//		new(getter.S3Detector),
		//		new(getter.GCSDetector),
		//		new(getter.FileDetector),
		//		new(getter.BitBucketDetector),
		//	},
		//	Getters: map[string]getter.Getter{
		//		"file":  new(getter.FileGetter),
		//		"git":   new(getter.GitGetter),
		//		"hg":    new(getter.HgGetter),
		//		"s3":    new(getter.S3Getter),
		//		"http":  httpGetter,
		//		"https": httpGetter,
		//	},
		return client.Get()
	}
	return nil
}

// ReadManifests locate & filter rendered manifests and print them in bytes.Buffer
func (p *GotplInflatorPlugin) ReadManifests(output *bytes.Buffer) error {
	for _, rs := range p.Dependencies {
		manifests, err := WalkMatch(rs.Dir, renderedManifestFilePattern)
		if os.IsNotExist(err) {
			//fmt.Println(" - no manifests found %s", rs.Dir)
			continue
		} else if err != nil {
			return err
		}
		for _, m := range manifests {
			mContent, err := ioutil.ReadFile(m)
			if err != nil {
				return err
			}
			// TODO - to function
			// test/parse rendered manifest
			mk := make(map[interface{}]interface{})
			err = yamlv2.Unmarshal([]byte(mContent), &mk)
			if err != nil {
				return err
			}
			// Kustomize lacks resource removal and multiple namespace manifests from dependencies cause `already registered id: ~G_v1_Namespace|~X|sre\`
			// https://kubectl.docs.kubernetes.io/faq/kustomize/eschewedfeatures/#removal-directives
			k := mk["kind"]
			if k != nil {
				kLcs := strings.ToLower(k.(string))
				if k == "namespace" || stringInSlice("!"+kLcs, rs.Kinds) {
					continue
				}
				if len(rs.Kinds) == 0 || stringInSlice(kLcs, rs.Kinds) {
					output.Write([]byte(mContent))
					output.WriteString("\n---\n")
				}
			}
		}
	}
	return nil
}

// CleanWorkdir cleanup temporary files plugin uses for build
func (p *GotplInflatorPlugin) CleanWorkdir() error {
	if os.Getenv("KUSTOMIZE_DEBUG") == "" {
		err := os.RemoveAll(p.TempDir)
		if err != nil {
			return err
		}
	} else {
		// DEBUG
		fmt.Println("# TempDir:", p.TempDir)
	}
	return nil
}

//
// PLUGIN UTILS

// stringInSlice boolean function
func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

// FlattenMap flatten context values to snake_case
// How about https://godoc.org/github.com/jeremywohl/flatten
func FlattenMap(prefix string, src map[string]interface{}, dest map[string]interface{}) {
	if len(prefix) > 0 {
		prefix += "_"
	}
	for k, v := range src {
		switch child := v.(type) {
		case map[string]interface{}:
			FlattenMap(prefix+k, child, dest)
		default:
			dest[prefix+k] = v
		}
	}
}

//WalkMatch returns list of files to render/process
func WalkMatch(root, pattern string) ([]string, error) {
	var matches []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
			// TODO recursive
		}
		if matched, err := filepath.Match(pattern, filepath.Base(path)); err != nil {
			return err
		} else if matched {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return matches, nil
}

//getRepoCreds read reference to credentials and returns go-getter URI
func getRepoCreds(repoCreds string) (string, error) {
	// not required if exec is used for go-getter => os env
	// for S3 you may want better way to get tokens, keys etc..
	// FIXME, builtin plugin, load repoCreds from plugin config?
	var cr = ""
	if repoCreds != "" {
		for _, e := range strings.Split(repoCreds, ",") {
			pair := strings.SplitN(e, "=", 2)
			//sshkey - for private git repositories
			if pair[0] == "sshkey" {
				key, err := ioutil.ReadFile(pair[1])
				if err != nil {
					return cr, err
				}
				keyb64 := base64.StdEncoding.EncodeToString([]byte(strings.TrimSpace(string(key))))
				cr = fmt.Sprintf("%s?sshkey=%s", cr, string(keyb64))
			}
		}
	}
	return cr, nil
}

// setFetchDst update rs.Dir with path where the repository is fetched (ie: tempdir/reponame-branch)
func (p *GotplInflatorPlugin) setFetchDst(idx int) (string, error) {

	// remote resource spec
	rs := p.Dependencies[idx]

	// identify fetched repo with branch/commit/etc..
	var reporefSpec = strings.SplitAfter(rs.Repo, "ref=")
	var reporef string
	if len(reporefSpec) > 1 {
		reporef = strings.Split(reporefSpec[1], "?")[0]
	}
	var reporeferal = fmt.Sprintf("%s-%s", rs.Name, reporef)
	var repotempdir = filepath.Join(p.TempDir, reporeferal)
	rs.Dir = repotempdir
	if rs.Path != "" {
		// if subpath in repo is defined
		rs.Dir = filepath.Join(p.TempDir, reporeferal, rs.Path)
	}
	p.Dependencies[idx].Dir = rs.Dir
	return rs.Dir, nil
}
