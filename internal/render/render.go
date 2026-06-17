// Package render runs kustomize build and parses the rendered manifests.
package render

import (
	"bytes"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

type Resource struct {
	Kind      string
	Name      string
	Namespace string
	Doc       map[string]any
}

// KustomizeFunc runs `kustomize build path` and returns its stdout.
type KustomizeFunc func(path string) ([]byte, error)

// Build renders the app at path and parses the multi-document output. The raw
// bytes are returned unchanged so they can be emitted verbatim on success.
func Build(path string, k KustomizeFunc) ([]byte, []Resource, error) {
	raw, err := k(path)
	if err != nil {
		return nil, nil, fmt.Errorf("kustomize build %s (fail-closed): %w", path, err)
	}
	resources, err := parse(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("parse rendered manifests: %w", err)
	}
	return raw, resources, nil
}

func parse(raw []byte) ([]Resource, error) {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	var out []Resource
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(doc) == 0 {
			continue // empty document between separators
		}
		out = append(out, toResource(doc))
	}
	return out, nil
}

func toResource(doc map[string]any) Resource {
	r := Resource{Doc: doc}
	r.Kind, _ = doc["kind"].(string)
	if md, ok := doc["metadata"].(map[string]any); ok {
		r.Name, _ = md["name"].(string)
		r.Namespace, _ = md["namespace"].(string)
	}
	return r
}
