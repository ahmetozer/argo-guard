package main

import future.keywords.in

# Cluster-scoped RBAC only from trusted infra repos.
deny contains msg if {
	input.kind in {"ClusterRole", "ClusterRoleBinding"}
	not context_repo_trusted
	msg := sprintf("%s/%s: cluster RBAC only allowed from trusted infra repos", [input.kind, input.metadata.name])
}

# Helper: is the deploying repo trusted?
# data.context.repo is injected by argo-guard via --data.
context_repo_trusted if {
	some r in data.trustedRepos
	r == data.context.repo
}

deny contains msg if {
	input.kind == "Service"
	input.spec.type == "LoadBalancer"
	msg := sprintf("Service/%s: LoadBalancer is not allowed", [input.metadata.name])
}

deny contains msg if {
	input.kind == "Deployment"
	some c in input.spec.template.spec.containers
	not c.resources.limits.memory
	msg := sprintf("Deployment/%s container %q must set a memory limit", [input.metadata.name, c.name])
}

warn contains msg if {
	input.kind == "Deployment"
	not input.metadata.labels.owner
	msg := sprintf("Deployment/%s should set an owner label", [input.metadata.name])
}
