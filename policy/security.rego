# Conftest policies for Elevarq Signals' Helm chart + Kubernetes
# manifests. Companion to `kube-linter lint` (which covers the
# CIS-aligned check set out of the box); these Rego rules express
# project-specific hardening decisions that don't have a
# kube-linter equivalent.
#
# Run locally via `bash scripts/preflight.sh kube-lint` (#164).
# Folded into CI via `.github/workflows/ci.yml`'s security-scan
# job.
#
# Spec: SECURITY.md "CI evidence (in progress)" — issue #164.

package main

import rego.v1

# ---------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------

# pod_spec returns the PodSpec for any workload kind that carries
# one. Conftest evaluates each YAML document as input; we extract
# the spec uniformly so the rules below stay short.
pod_spec contains spec if {
	input.kind == "Deployment"
	spec := input.spec.template.spec
}

pod_spec contains spec if {
	input.kind == "StatefulSet"
	spec := input.spec.template.spec
}

pod_spec contains spec if {
	input.kind == "DaemonSet"
	spec := input.spec.template.spec
}

pod_spec contains spec if {
	input.kind == "Job"
	spec := input.spec.template.spec
}

pod_spec contains spec if {
	input.kind == "CronJob"
	spec := input.spec.jobTemplate.spec.template.spec
}

pod_spec contains spec if {
	input.kind == "Pod"
	spec := input.spec
}

# ---------------------------------------------------------------
# Project policy
# ---------------------------------------------------------------

# arq-signals collectors never call the Kubernetes API at runtime
# (read-only PostgreSQL queries + local SQLite + HTTP). Mounting
# the ServiceAccount token expands blast radius for zero benefit.
# Standard container hardening rule.
deny contains msg if {
	some spec in pod_spec
	not spec.automountServiceAccountToken == false
	msg := sprintf(
		"%s/%s: automountServiceAccountToken MUST be false (no runtime Kubernetes-API calls; defence in depth, mirrors Elevarq #443)",
		[input.kind, input.metadata.name],
	)
}

# Reject privileged: true and privilege-escalation.
deny contains msg if {
	some spec in pod_spec
	some c in spec.containers
	c.securityContext.privileged == true
	msg := sprintf(
		"%s/%s container %q: privileged: true is forbidden",
		[input.kind, input.metadata.name, c.name],
	)
}

deny contains msg if {
	some spec in pod_spec
	some c in spec.containers
	c.securityContext.allowPrivilegeEscalation == true
	msg := sprintf(
		"%s/%s container %q: allowPrivilegeEscalation MUST be false",
		[input.kind, input.metadata.name, c.name],
	)
}

# Containers must drop ALL Linux capabilities. Elevarq Signals does
# not need any capability; the read-only PG queries + HTTP API
# run in user-space.
deny contains msg if {
	some spec in pod_spec
	some c in spec.containers
	not has_cap_drop_all(c)
	msg := sprintf(
		"%s/%s container %q: securityContext.capabilities.drop MUST include \"ALL\"",
		[input.kind, input.metadata.name, c.name],
	)
}

has_cap_drop_all(c) if {
	some cap in c.securityContext.capabilities.drop
	cap == "ALL"
}

# Containers must run as non-root.
deny contains msg if {
	some spec in pod_spec
	not runs_as_non_root(spec)
	msg := sprintf(
		"%s/%s: pod or container securityContext MUST set runAsNonRoot: true",
		[input.kind, input.metadata.name],
	)
}

runs_as_non_root(spec) if {
	spec.securityContext.runAsNonRoot == true
}

runs_as_non_root(spec) if {
	some c in spec.containers
	c.securityContext.runAsNonRoot == true
}

# hostNetwork / hostPID / hostIPC are escape vectors with no
# legitimate use in arq-signals workloads.
deny contains msg if {
	some spec in pod_spec
	spec.hostNetwork == true
	msg := sprintf(
		"%s/%s: hostNetwork is forbidden",
		[input.kind, input.metadata.name],
	)
}

deny contains msg if {
	some spec in pod_spec
	spec.hostPID == true
	msg := sprintf(
		"%s/%s: hostPID is forbidden",
		[input.kind, input.metadata.name],
	)
}

deny contains msg if {
	some spec in pod_spec
	spec.hostIPC == true
	msg := sprintf(
		"%s/%s: hostIPC is forbidden",
		[input.kind, input.metadata.name],
	)
}
