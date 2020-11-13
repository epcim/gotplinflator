// Copyright 2019 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

//go:generate pluginator
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig"
	"sigs.k8s.io/kustomize/api/filesys"
	"sigs.k8s.io/kustomize/v3/pkg/ifc"
	"sigs.k8s.io/kustomize/v3/pkg/resmap"
	"sigs.k8s.io/yaml"

	"path/filepath"
)

var (
	gotplFilePattern    = "*.t*pl"
	manifestFilePattern = "*.y*ml"
)

var SprigCustomFuncs = map[string]interface{}{
	"handleEnvVars": func(rawEnvs interface{}) map[string]string {
		envs := map[string]string{}
		if str, ok := rawEnvs.(string); ok {
			err := json.Unmarshal([]byte(str), &envs)
			if err != nil {
				log.Fatal("failed to unmarshal Envs, %s", err)
			}
		}
		return envs
	},
}

type remoteResource struct {
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
	TemplatePattern string `json:"templatePatt,omitempty" yaml:"templatePatt,omitempty"`
	TemplateOpts    string `json:"templateOpts,omitempty" yaml:"templateOpts,omitempty"`

	// Dir is where the resource is cloned
	Dir string
}

// Getter is a function that can gets resource
type remoteTargetGetter func(rs *remoteResource) error

type plugin struct {
	rf           *resmap.Factory
	ldr          ifc.Loader
	Name         string                 `json:"name,omitempty" yaml:"name,omitempty"`
	Dependencies []remoteResource       `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	Values       map[string]interface{} `json:"values,omitempty" yaml:"values,omitempty"`
	TempDir      string
}

//KustomizePlugin xxx
//noinspection GoUnusedGlobalVariable
//nolint: golint
var KustomizePlugin plugin

// FlattenMap flatten context values to snake_case
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

func RenderGotpl(t string, context *map[string]interface{}) {

	// read template
	tContent, err := ioutil.ReadFile(t)
	if err != nil {
		log.Fatal(err)
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
	err = tpl.Execute(&rb, context)
	if err != nil {
		log.Fatal("Failed to render template: %s", err)
		os.Exit(1)
	}

	// write
	tBasename := strings.TrimSuffix(t, filepath.Ext(t))
	err = ioutil.WriteFile(tBasename, rb.Bytes(), 0640)
	if err != nil {
		log.Fatal(err)
	}
}

//getRepoCreds returns go-getter URI based on required credential profile (see: plugin configuration)
func getRepoCreds(repoCreds string) string {
	// not required if exec plugin => os env
	// for S3 you may want better way to get tokens, keys etc..
	// TODO, builtin plugin, load repoCreds from plugin config
	var cr = ""
	if repoCreds != "" {
		for _, e := range strings.Split(repoCreds, ",") {
			pair := strings.SplitN(e, "=", 2)
			if pair[0] == "sshkey" {
				key, err := ioutil.ReadFile(pair[1])
				if err != nil {
					log.Fatal(err)
				}
				keyb64 := base64.StdEncoding.EncodeToString([]byte(strings.TrimSpace(string(key))))
				cr = fmt.Sprintf("%s?sshkey=%s", cr, string(keyb64))
			}
		}
	}
	return cr
}

// fetchRemoteResource fetch locally remote dependency
func fetchRemoteResource(rs *remoteResource, tempDir *string) error {

	fmt.Println("# Fetching:", rs.Name)

	var repoTempDir = filepath.Join(*tempDir, "repo", rs.Name)
	rs.Dir = repoTempDir
	if rs.Path != "" {
		// if subPath defined
		rs.Dir = filepath.Join(*tempDir, "repo", rs.Name, rs.Path)
	}

	//handle credentials
	//https://github.com/hashicorp/go-getter#git-git
	//getterCreds := getRepoCreds(rs.RepoCreds)

	// skip fetch if present and not forced
	_, err := os.Stat(rs.Dir)
	if err == nil && rs.Pull != "Always" && os.Getenv("KUSTOMIZE_GOTPLINFLATOR_PULL") != "Always" {
		fmt.Println("#- skipped, already exist")
		return nil
	}

	// prepare dir
	if os.IsNotExist(err) {
		_ = os.MkdirAll(repoTempDir, 0770)
	} else {
		err := os.RemoveAll(repoTempDir)
		if err != nil {
			log.Fatal(err)
		}
	}

	//fetch
	fmt.Println("# go-getter", rs.Repo, repoTempDir)
	cmd := exec.Command("go-getter", rs.Repo, repoTempDir)
	//cmd.Stdout = os.Stdout
	//cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		log.Fatalf("go-getter failed to clone repo %s\n", err)
		return err
	}
	return nil

	//fetch2 - to be used within go native plugin
	//
	//ISSUE: does not properly handle gitlab, no credentials to fetch
	//
	//pwd, err := os.Getwd()
	//if err != nil {
	//	log.Fatalf("Error getting wd: %s", err)
	//}
	//opts := []getter.ClientOption{}
	//client := &getter.Client{
	//	Ctx:  context.TODO(),
	//	Src:  rs.Repo + getterCreds,
	//	Dst:  repoTempDir,
	//	Pwd:  pwd,
	//	Mode: getter.ClientModeAny,
	//	Detectors: []getter.Detector{
	//		new(getter.GitHubDetector),
	//		new(getter.GitLabDetector),
	//		new(getter.GitDetector),
	//		new(getter.S3Detector),
	//		new(getter.GCSDetector),
	//		new(getter.FileDetector),
	//		new(getter.BitBucketDetector),
	//	},
	//	Options: opts,
	//}
	//return client.Get()
}

func (p *plugin) Config(
	ldr ifc.Loader, rf *resmap.Factory, c []byte) error {
	p.rf = rf
	p.ldr = ldr
	return yaml.Unmarshal(c, p)
}

func (p *plugin) Generate() (resmap.ResMap, error) {

	//DEBUG
	//fmt.Println()
	//for _, e := range os.Environ() {
	//    pair := strings.SplitN(e, "=", 2)
	//    fmt.Printf("%s='%s'\n",pair[0], pair[1])
	//}

	//MAIN
	// tempdir
	var pluginConfigRoot = os.Getenv("KUSTOMIZE_PLUGIN_CONFIG_ROOT")
	//pwd, err := os.Getwd()
	//if err != nil {
	//	log.Fatalf("Error getting pwd: %s", err)
	//}

	if pluginConfigRoot != "" && os.Getenv("KUSTOMIZE_DEBUG") != "" {
		p.TempDir = filepath.Join(pluginConfigRoot, ".temp")
		if _, err = os.Stat(p.TempDir); os.IsNotExist(err) {
			err = os.MkdirAll(p.TempDir, 0770)
		}
	} else {
		var _tempDir filesys.ConfirmedDir
		_tempDir, err = filesys.NewTmpConfirmedDir()
		p.TempDir = _tempDir.String()
	}
	if err != nil {
		fmt.Errorf("Failed to create temp dir: %s", p.TempDir)
		os.Exit(1)
	}
	fmt.Println("# TempDir:", p.TempDir)

	// normalize context values used for template rendering
	nv := make(map[string]interface{})
	FlattenMap("", p.Values, nv)
	p.Values = nv

	//DEBUG
	//for k, v := range p.Values {
	//	fmt.Printf("%s:%s\n", k, v)
	//}

	// Process dependencies
	for idx := range p.Dependencies {
		err := fetchRemoteResource(&p.Dependencies[idx], &p.TempDir)
		if err != nil {
			log.Fatalf("Error getting remote repo: %s", err)
			return nil, err
		}
	}

	// render
	for _, rs := range p.Dependencies {

		fmt.Println("# Rendering:", rs.Name)

		// find templates
		if rs.TemplatePattern != "" {
			gotplFilePattern = rs.TemplatePattern
		}
		templates, err := WalkMatch(rs.Dir, gotplFilePattern)
		if os.IsNotExist(err) {
			fmt.Println("# - no templates found")
			continue
		} else if err != nil {
			log.Fatal(err)
		}

		// actual render
		for _, t := range templates {
			fmt.Printf("# - %s\n", strings.SplitAfter(t, p.TempDir+"/repo/")[1])

			RenderGotpl(t, &p.Values)
		}
	}

	// print out
	var output bytes.Buffer
	output.WriteString("\n---\n")
	for _, rs := range p.Dependencies {
		manifests, err := WalkMatch(rs.Dir, manifestFilePattern)
		if os.IsNotExist(err) {
			fmt.Println(" - no manifests found")
			continue
		} else if err != nil {
			log.Fatal(err)
		}
		for _, m := range manifests {
			mContent, err := ioutil.ReadFile(m)
			if err != nil {
				log.Fatal(err)
			}
			output.Write([]byte(mContent))
			output.WriteString("\n---\n")
		}
	}
	fmt.Print(output.String())

	// cleanup
	// TODO, defer on builtin plugin
	if os.Getenv("KUSTOMIZE_DEBUG") == "" {
		err := os.RemoveAll(p.TempDir)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		fmt.Println("# TempDir:", p.TempDir)
	}
}