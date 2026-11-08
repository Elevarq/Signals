# Acceptance — Cloud API Port Binding (#719)

Spec: `cloud-api-binding.md`. Test: `tests/signals_cloud_api_binding_test.go`
(config consistency across the Azure/GCP bridge-networking templates).

```yaml
- id: AC-CAB-01            # normal — daemon binds the container interface
  ref: [CAB-R001, B1]
  given: the Azure/GCP terraform + bicep deployment templates
  when: their api.listen_addr is inspected
  then: it is 0.0.0.0:8081 and never 127.0.0.1:8081 inside the container
  test: TestCloudTemplates_BindContainerInterface

- id: AC-CAB-02            # boundary — host publish stays loopback-only
  ref: [CAB-R002, B2, INV-CAB-01]
  given: the same templates
  when: their docker host publish is inspected
  then: it is 127.0.0.1:8081:8081 and never 0.0.0.0:8081:8081
  test: TestCloudTemplates_HostPublishStaysLoopback
```
