package main

import future.keywords.in

test_loadbalancer_denied if {
	some msg in deny with input as {"kind": "Service", "metadata": {"name": "web"}, "spec": {"type": "LoadBalancer"}}
	contains(msg, "LoadBalancer is not allowed")
}

test_clusterrole_denied_for_untrusted_repo if {
	count(deny) > 0 with input as {"kind": "ClusterRole", "metadata": {"name": "x"}}
		with data.context as {"repo": "https://git.corp/team-a/app.git"}
		with data.trustedRepos as ["https://git.corp/infra/platform.git"]
}

test_clusterrole_allowed_for_trusted_repo if {
	count(deny) == 0 with input as {"kind": "ClusterRole", "metadata": {"name": "x"}}
		with data.context as {"repo": "https://git.corp/infra/platform.git"}
		with data.trustedRepos as ["https://git.corp/infra/platform.git"]
}

test_missing_memory_limit_denied if {
	some msg in deny with input as {
		"kind": "Deployment",
		"metadata": {"name": "web"},
		"spec": {"template": {"spec": {"containers": [{"name": "app", "resources": {}}]}}},
	}
	contains(msg, "must set a memory limit")
}
