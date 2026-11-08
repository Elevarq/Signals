# Acceptance — CI Supply-Chain Tool Pinning (#733)

Spec: `ci-tool-pinning.md`. Tests: `tests/signals_ci_pinned_tool_test.go`
(the verifier accepts a matching fixture tarball and rejects altered / missing
/ malformed inputs; `ci.yml` installs both tools through the verifier).

```yaml
- id: AC-SCT-01            # normal — matching digest extracts and succeeds
  ref: [SCT-R001, B1, INV-SCT-01]
  given: a fixture tarball whose SHA-256 equals the pinned expected digest
  when: install-pinned-tool.sh runs with that digest, member and dest
  then: the member is extracted into dest and exit status is 0
  test: TestCIPinnedTool_MatchingDigestExtracts

- id: AC-SCT-02            # failure — altered bytes fail closed before extraction
  ref: [SCT-R002, B2, INV-SCT-01]
  given: a fixture tarball whose bytes differ from the pinned digest
  when: install-pinned-tool.sh runs with the original (now wrong) digest
  then: exit is non-zero and no member is extracted into dest
  test: TestCIPinnedTool_AlteredBytesRejected

- id: AC-SCT-03            # failure — missing download fails closed
  ref: [SCT-R002, SCT-R003, B3]
  given: a file:// URL that does not exist
  when: install-pinned-tool.sh runs
  then: exit is non-zero and nothing is extracted
  test: TestCIPinnedTool_MissingDownloadRejected

- id: AC-SCT-04            # failure — malformed pin fails closed before download
  ref: [SCT-R002, B4]
  given: an expected-digest argument that is not 64 hex chars
  when: install-pinned-tool.sh runs
  then: exit is non-zero and nothing is extracted
  test: TestCIPinnedTool_MalformedDigestRejected

- id: AC-SCT-05            # boundary — verified tarball missing the member fails closed
  ref: [SCT-R002, B5]
  given: a tarball that matches its digest but lacks the requested member
  when: install-pinned-tool.sh runs with the correct digest
  then: exit is non-zero and no partial install is left
  test: TestCIPinnedTool_MissingMemberRejected

- id: AC-SCT-06            # structural — CI installs both tools through the verifier
  ref: [SCT-R001, SCT-R004, B6, INV-SCT-02]
  given: .github/workflows/ci.yml
  when: the kube-linter and conftest install steps are inspected
  then: both call install-pinned-tool.sh with a 64-hex pinned digest, and
        neither pipes a download straight into tar
  test: TestCIPinnedTool_WorkflowPinsBothTools
```
