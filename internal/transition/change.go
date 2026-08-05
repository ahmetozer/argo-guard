// Package transition builds policy inputs from two rendered desired states.
package transition

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/ahmetozer/argo-guard/internal/render"
	"gopkg.in/yaml.v3"
)

const APIVersion = "guard.ahmetozer.github.io/v1alpha1"

type ObjectReference struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
	Namespace  string `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	Name       string `yaml:"name" json:"name"`
}

type Metadata struct {
	Name      string `yaml:"name" json:"name"`
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`
}

type Spec struct {
	Operation string          `yaml:"operation" json:"operation"`
	Resource  ObjectReference `yaml:"resource" json:"resource"`
	Before    map[string]any  `yaml:"before,omitempty" json:"before,omitempty"`
	After     map[string]any  `yaml:"after,omitempty" json:"after,omitempty"`
}

// Change is a synthetic Conftest document evaluated only by transition bundles.
type Change struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Spec       Spec     `yaml:"spec" json:"spec"`
}

// Summary is the lightweight change index exposed to policies as
// data.transition.changes. Full objects remain in beforeResources and
// afterResources so each rendered manifest is stored only once.
type Summary struct {
	Operation string          `yaml:"operation" json:"operation"`
	Resource  ObjectReference `yaml:"resource" json:"resource"`
}

type indexedResource struct {
	ref ObjectReference
	doc map[string]any
}

func key(group string, ref ObjectReference) string {
	return group + "\x00" + ref.Kind + "\x00" + ref.Namespace + "\x00" + ref.Name
}

func apiGroup(apiVersion string) (string, error) {
	parts := strings.Split(apiVersion, "/")
	switch {
	case len(parts) == 1 && parts[0] != "":
		return "", nil // core API, for example v1
	case len(parts) == 2 && parts[0] != "" && parts[1] != "":
		return parts[0], nil
	default:
		return "", fmt.Errorf("invalid apiVersion %q", apiVersion)
	}
}

func index(resources []render.Resource) (map[string]indexedResource, error) {
	out := make(map[string]indexedResource, len(resources))
	for i, resource := range resources {
		ref := ObjectReference{
			APIVersion: resource.APIVersion,
			Kind:       resource.Kind,
			Namespace:  resource.Namespace,
			Name:       resource.Name,
		}
		if ref.APIVersion == "" || ref.Kind == "" || ref.Name == "" {
			return nil, fmt.Errorf("rendered document %d must have apiVersion, kind, and metadata.name", i+1)
		}
		group, err := apiGroup(ref.APIVersion)
		if err != nil {
			return nil, fmt.Errorf("rendered document %d: %w", i+1, err)
		}
		k := key(group, ref)
		if _, exists := out[k]; exists {
			return nil, fmt.Errorf("duplicate rendered resource group=%q kind=%q namespace=%q name=%q", group, ref.Kind, ref.Namespace, ref.Name)
		}
		out[k] = indexedResource{ref: ref, doc: resource.Doc}
	}
	return out, nil
}

// Diff returns deterministic create, update, and delete change documents.
func Diff(before, after []render.Resource) ([]Change, error) {
	oldIndex, err := index(before)
	if err != nil {
		return nil, fmt.Errorf("index previous manifests: %w", err)
	}
	newIndex, err := index(after)
	if err != nil {
		return nil, fmt.Errorf("index current manifests: %w", err)
	}

	keys := make([]string, 0, len(oldIndex)+len(newIndex))
	seen := map[string]bool{}
	for k := range oldIndex {
		keys = append(keys, k)
		seen[k] = true
	}
	for k := range newIndex {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	changes := make([]Change, 0, len(keys))
	for _, k := range keys {
		oldResource, hadBefore := oldIndex[k]
		newResource, hasAfter := newIndex[k]
		if hadBefore && hasAfter && reflect.DeepEqual(oldResource.doc, newResource.doc) {
			continue
		}

		ref := oldResource.ref
		operation := "Delete"
		if hasAfter {
			ref = newResource.ref
			operation = "Create"
			if hadBefore {
				operation = "Update"
			}
		}
		change := Change{
			APIVersion: APIVersion,
			Kind:       "ManifestChange",
			Metadata:   Metadata{Name: ref.Name, Namespace: ref.Namespace},
			Spec:       Spec{Operation: operation, Resource: ref},
		}
		if hadBefore {
			change.Spec.Before = oldResource.doc
		}
		if hasAfter {
			change.Spec.After = newResource.doc
		}
		changes = append(changes, change)
	}
	return changes, nil
}

// Summaries returns the operation and stable identity of each change.
func Summaries(changes []Change) []Summary {
	out := make([]Summary, 0, len(changes))
	for _, change := range changes {
		out = append(out, Summary{Operation: change.Spec.Operation, Resource: change.Spec.Resource})
	}
	return out
}

// Encode serializes changes as a multi-document YAML stream for Conftest.
func Encode(changes []Change) ([]byte, error) {
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	defer enc.Close()
	for _, change := range changes {
		if err := enc.Encode(change); err != nil {
			return nil, fmt.Errorf("encode manifest changes: %w", err)
		}
	}
	return out.Bytes(), nil
}
