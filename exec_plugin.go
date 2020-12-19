// Copyright 2019 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	//"regexp"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig"
	"sigs.k8s.io/kustomize/api/filesys"
	"sigs.k8s.io/yaml"

	yamlv2 "gopkg.in/yaml.v2"
	//getter "github.com/hashicorp/go-getter"
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

// remoteResource structs describe dependency to fetch and render
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
	// kinds
	SkipKinds []string `json:"skipKinds,omitempty" yaml:"skipKinds,omitempty"`

	// Dir is where the resource is cloned
	Dir string
}

type plugin struct {
	Name         string                 `json:"name,omitempty" yaml:"name,omitempty"`
	Dependencies []remoteResource       `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	Values       map[string]interface{} `json:"values,omitempty" yaml:"values,omitempty"`
	TempDir      string
}

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
		log.Fatal("Read template failed: %s", err)
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
		log.Fatal("Write template failed: %s", err)
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

// hash generate fowler–noll–vo hash from string
func hash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

// fetchRemoteResource fetch locally remote dependency
func fetchRemoteResource(rs *remoteResource, tempDir *string) error {

	fmt.Println("# Fetching:", rs.Name)

	// identify fetched repo with branch/commit/etc..
	var repoRef = strings.Split(strings.SplitAfter(rs.Repo, "ref=")[1], "?")[0]
	if repoRef == "" { // otherwise hash whole repo url
		repoRef = fmt.Sprintf("%d", hash(rs.Repo))
	}
	var repoReferal = fmt.Sprintf("%s-%s", rs.Name, repoRef)
	var repoTempDir = filepath.Join(*tempDir, repoReferal)
	rs.Dir = repoTempDir
	if rs.Path != "" {
		// if subPath defined
		rs.Dir = filepath.Join(*tempDir, repoReferal, rs.Path)
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

	// cleanup/create temp dir
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

// main executes as Kustomize exec plugin
func main() {

	//DEBUG
	//for _, e := range os.Environ() {
	//	pair := strings.SplitN(e, "=", 2)
	//	fmt.Printf("#DEBUG %s='%s'\n", pair[0], pair[1])
	//}

	if len(os.Args) != 2 {
		fmt.Println("received too few args:", os.Args)
		fmt.Println("always invoke this via kustomize plugins")
		os.Exit(1)
	}

	// ignore the first file name argument
	// load the second argument, the file path
	content, err := ioutil.ReadFile(os.Args[1])

	if err != nil {
		fmt.Println("unable to read in manifest", os.Args[1])
		os.Exit(1)
	}

	var p plugin

	err = yaml.Unmarshal(content, &p)

	if err != nil {
		fmt.Printf("error unmarshalling manifest content: %q \n%s\n", err, content)
		os.Exit(1)
	}

	if p.Dependencies == nil {
		fmt.Println("missing the required 'dependencies' key in the manifest")
		os.Exit(1)
	}

	//MAIN

	// FIXTURES
	var pluginConfigRoot = os.Getenv("KUSTOMIZE_PLUGIN_CONFIG_ROOT")
	if os.Getenv("KUSTOMIZE_GOTPLINFLATOR_ROOT") == "" {
		var envsPath = strings.SplitAfter(pluginConfigRoot, "/envs/")
		if len(envsPath) > 1 {
			os.Setenv("KUSTOMIZE_GOTPLINFLATOR_ROOT", filepath.Join(envsPath[0], "../repos"))
			os.Setenv("ENV", strings.Split(envsPath[1], "/")[0])
		}
	}
	var gotplInflatorRoot = os.Getenv("KUSTOMIZE_GOTPLINFLATOR_ROOT")

	// tempdir
	if gotplInflatorRoot != "" {
		//p.TempDir = filepath.Join(gotplInflatorRoot, os.Getenv("ENV"))
		p.TempDir = filepath.Join(gotplInflatorRoot)
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
			os.Exit(1)
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
			log.Fatal("Rendering failed", err)
		}

		// actual render
		for _, t := range templates {
			fmt.Printf("# - %s\n", strings.SplitAfter(t, p.TempDir)[1])

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
			log.Fatal("Print out failed", err)
		}
		for _, m := range manifests {
			mContent, err := ioutil.ReadFile(m)
			if err != nil {
				log.Fatal("Read manifest failed: %s", err)
			}

			// test/parse rendered manifest
			mk := make(map[interface{}]interface{})
			err = yamlv2.Unmarshal([]byte(mContent), &mk)
			if err != nil {
				log.Fatalf("Failed to load rendered manifest: %v", err)
			}
			// Kustomize lacks resource removal and multiple namespace manifests from dependencies cause `already registered id: ~G_v1_Namespace|~X|sre\`
			// https://kubectl.docs.kubernetes.io/faq/kustomize/eschewedfeatures/#removal-directives
			k := mk["kind"]
			if k != nil {
				if len(rs.SkipKinds) == 0 { // by default excluded Kinds
					rs.SkipKinds = append(rs.SkipKinds, "namespace")
					rs.SkipKinds = append(rs.SkipKinds, "secret")
				}
				if !stringInSlice(strings.ToLower(k.(string)), rs.SkipKinds) {
					output.Write([]byte(mContent))
					output.WriteString("\n---\n")
				}
			}
		}
	}
	fmt.Print(output.String())

	// cleanup
	if os.Getenv("KUSTOMIZE_DEBUG") == "" {
		if gotplInflatorRoot != "" {
			// do not remove already fetched repositories
		} else {
			err := os.RemoveAll(p.TempDir)
			if err != nil {
				log.Fatal("Cleanup failed: %s", err)
			}
		}
	} else {
		fmt.Println("# TempDir:", p.TempDir)
	}
}
