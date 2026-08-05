package main

import rego.v1

test_scale_to_zero_denied if {
	some msg in deny with input as {
		"kind": "ManifestChange",
		"spec": {
			"operation": "Update",
			"before": {"kind": "Deployment", "metadata": {"name": "web"}, "spec": {"replicas": 2}},
			"after": {"kind": "Deployment", "metadata": {"name": "web"}, "spec": {"replicas": 0}},
		},
	}
	contains(msg, "cannot transition to replicas=0")
}

test_existing_zero_allowed if {
	count(deny) == 0 with input as {
		"kind": "ManifestChange",
		"spec": {
			"operation": "Update",
			"before": {"kind": "Deployment", "metadata": {"name": "web"}, "spec": {"replicas": 0}},
			"after": {"kind": "Deployment", "metadata": {"name": "web"}, "spec": {"replicas": 0}},
		},
	}
}

test_new_zero_denied if {
	count(deny) > 0 with input as {
		"kind": "ManifestChange",
		"spec": {
			"operation": "Create",
			"after": {"kind": "StatefulSet", "metadata": {"name": "database"}, "spec": {"replicas": 0}},
		},
	}
}
