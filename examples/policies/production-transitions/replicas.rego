package main

import rego.v1

previously_zero if {
	input.spec.before != null
	object.get(input.spec.before.spec, "replicas", 1) == 0
}

deny contains msg if {
	input.kind == "ManifestChange"
	input.spec.after.kind in {"Deployment", "StatefulSet"}
	object.get(input.spec.after.spec, "replicas", 1) == 0
	not previously_zero
	msg := sprintf("%s/%s cannot transition to replicas=0", [input.spec.after.kind, input.spec.after.metadata.name])
}
