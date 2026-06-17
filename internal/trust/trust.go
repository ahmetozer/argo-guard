// Package trust builds the spoof-proof deployment context from Argo CD
// environment variables. Nothing here ever reads manifest content.
package trust

import (
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
