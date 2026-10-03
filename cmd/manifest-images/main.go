package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
)

func imagesFromReader(r io.Reader) ([]string, error) {
	decoder := k8syaml.NewYAMLOrJSONDecoder(r, 4096)
	var images []string

	for {
		var doc any
		if err := decoder.Decode(&doc); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if doc == nil {
			continue
		}
		collectImages(doc, &images)
	}

	sort.Strings(images)
	return images, nil
}

func collectImages(value any, images *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "image" {
				if image, ok := child.(string); ok {
					image = strings.TrimSpace(image)
					if image != "" {
						*images = append(*images, image)
					}
				}
			}
			collectImages(child, images)
		}
	case []any:
		for _, child := range typed {
			collectImages(child, images)
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: manifest-images <manifest.yaml> [...]")
		os.Exit(2)
	}

	for _, path := range os.Args[1:] {
		file, err := os.Open(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "open %s: %v\n", path, err)
			os.Exit(1)
		}

		images, err := imagesFromReader(file)
		closeErr := file.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "parse %s: %v\n", path, err)
			os.Exit(1)
		}
		if closeErr != nil {
			fmt.Fprintf(os.Stderr, "close %s: %v\n", path, closeErr)
			os.Exit(1)
		}

		for _, image := range images {
			fmt.Println(image)
		}
	}
}
