package main

import rego.v1

pod_spec(resource) := spec if {
	resource.kind in {"Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "ReplicationController", "Job"}
	spec := resource.spec.template.spec
}

pod_spec(resource) := spec if {
	resource.kind == "CronJob"
	spec := resource.spec.jobTemplate.spec.template.spec
}

resource_namespace(resource) := object.get(resource.metadata, "namespace", data.context.namespace)

reference_namespace(reference) := object.get(reference, "namespace", data.context.namespace)

deny contains msg if {
	input.kind == "ManifestChange"
	input.spec.operation == "Delete"
	input.spec.resource.kind == "ServiceAccount"

	some workload in data.transition.afterResources
	spec := pod_spec(workload)
	object.get(spec, "serviceAccountName", "default") == input.spec.resource.name
	resource_namespace(workload) == reference_namespace(input.spec.resource)

	msg := sprintf("ServiceAccount/%s is still referenced by %s/%s", [input.spec.resource.name, workload.kind, workload.metadata.name])
}
