// Package trust builds the spoof-proof deployment context from Argo CD
// environment variables. Nothing here ever reads manifest content.
package trust

import (
	"fmt"
	"os"
	"strings"
)

// Context is the trust context injected into policy evaluation and used to
// select policy bundles. All fields originate from Argo-provided env vars.
type Context struct {
	Repo      string            `json:"repo"`
	Project   string            `json:"project"`
	Namespace string            `json:"namespace"`
	AppLabels map[string]string `json:"appLabels"`
}

// AppRef identifies which Application and which revision Argo CD is currently
// rendering. Like Context, every field comes from Argo rather than from
// manifest content — transition policies pick their enforcement baseline from
// it, so a developer must not be able to influence it by editing YAML.
//
// AppRef is deliberately NOT serialized into data.context: it is an input to
// argo-guard's own decisions, not part of the documented policy data contract.
// Widening that contract should be a separate, documented change.
type AppRef struct {
	Name       string
	Revision   string
	SourcePath string
}

// AppRefFromEnv reads the Application identity Argo injects per generation.
func AppRefFromEnv(getenv func(string) string) AppRef {
	return AppRef{
		Name:       getenv("ARGOCD_APP_NAME"),
		Revision:   getenv("ARGOCD_APP_REVISION"),
		SourcePath: getenv("ARGOCD_APP_SOURCE_PATH"),
	}
}

// Validate reports whether enough identity is present to anchor a transition to
// a specific Application and revision. SourcePath is not required: an empty
// value legitimately means the repo root.
func (a AppRef) Validate() error {
	var missing []string
	if a.Name == "" {
		missing = append(missing, "ARGOCD_APP_NAME")
	}
	if a.Revision == "" {
		missing = append(missing, "ARGOCD_APP_REVISION")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s must be set", strings.Join(missing, " and "))
	}
	return nil
}

const envLabelPrefix = "ARGOCD_ENV_"

// FromEnv assembles a Context. getenv is injected for testing; pass os.Getenv
// in production. AppLabels are sourced from ARGOCD_ENV_* plugin parameters
// (lowercased keys), since Argo does not expose Application labels to a CMP.
func FromEnv(getenv func(string) string) Context {
	c := Context{
		Repo:      getenv("ARGOCD_APP_SOURCE_REPO_URL"),
		Project:   getenv("ARGOCD_APP_PROJECT_NAME"),
		Namespace: getenv("ARGOCD_APP_NAMESPACE"),
		AppLabels: map[string]string{},
	}
	for _, kv := range os.Environ() {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		key, val := kv[:eq], kv[eq+1:]
		if strings.HasPrefix(key, envLabelPrefix) {
			c.AppLabels[strings.ToLower(strings.TrimPrefix(key, envLabelPrefix))] = val
		}
	}
	return c
}
