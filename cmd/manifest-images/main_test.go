package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestImagesFromReaderFindsFlowStyleAndMultiDocumentImages(t *testing.T) {
	manifest := `
apiVersion: v1
kind: Pod
metadata:
  name: flow
spec: {containers: [{name: app, image: busybox:latest}]}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: regular
spec:
  template:
    spec:
      initContainers:
        - name: init
          image: registry.example/init@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
      containers:
        - name: app
          image: registry.example/app@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
`

	got, err := imagesFromReader(strings.NewReader(manifest))
	if err != nil {
		t.Fatalf("imagesFromReader() error = %v", err)
	}

	want := []string{
		"busybox:latest",
		"registry.example/init@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"registry.example/app@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("imagesFromReader() = %#v, want %#v", got, want)
	}
}

func TestImagesFromReaderRejectsInvalidYAML(t *testing.T) {
	if _, err := imagesFromReader(strings.NewReader("spec: [")); err == nil {
		t.Fatal("imagesFromReader() error = nil, want parse error")
	}
}
