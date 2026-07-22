package main

import rego.v1

test_referenced_service_account_delete_denied if {
	some msg in deny with input as {
		"kind": "ManifestChange",
		"spec": {
			"operation": "Delete",
			"resource": {"kind": "ServiceAccount", "namespace": "payments", "name": "payment-api"},
		},
	}
		with data.context as {"namespace": "payments"}
		with data.transition as {
			"afterResources": [{
				"kind": "Deployment",
				"metadata": {"name": "web", "namespace": "payments"},
				"spec": {"template": {"spec": {"serviceAccountName": "payment-api"}}},
			}],
		}
	contains(msg, "still referenced by Deployment/web")
}

test_unreferenced_service_account_delete_allowed if {
	count(deny) == 0 with input as {
		"kind": "ManifestChange",
		"spec": {
			"operation": "Delete",
			"resource": {"kind": "ServiceAccount", "namespace": "payments", "name": "old-api"},
		},
	}
		with data.context as {"namespace": "payments"}
		with data.transition as {
			"afterResources": [{
				"kind": "Deployment",
				"metadata": {"name": "web", "namespace": "payments"},
				"spec": {"template": {"spec": {"serviceAccountName": "payment-api"}}},
			}],
		}
}

test_default_service_account_reference_denied if {
	count(deny) > 0 with input as {
		"kind": "ManifestChange",
		"spec": {
			"operation": "Delete",
			"resource": {"kind": "ServiceAccount", "name": "default"},
		},
	}
		with data.context as {"namespace": "payments"}
		with data.transition as {
			"afterResources": [{
				"kind": "CronJob",
				"metadata": {"name": "billing"},
				"spec": {"jobTemplate": {"spec": {"template": {"spec": {}}}}},
			}],
		}
}
