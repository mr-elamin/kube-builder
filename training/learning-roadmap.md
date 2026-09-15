# Learning roadmap for the bssconnects platform

Everything to learn for the project, **in the order it pays off**. Each topic says why the project
needs it, what you can skip given what you already know, and a small task that proves you learned
it.

Your starting point, from your background: CKA/CKS, Docker, Go, CRDs and mutating webhooks, Istio
multi-cluster, SPIRE, Falco, Harbor and Keycloak administration. So this skips Kubernetes basics and
focuses on the gap between *operating* these systems and *building a product on top of them*.

---

## Phase A — The operator (this course, stages 00–12)

### A1. Go, at the depth a controller needs

**Why:** every platform service and the operator are Go.
**Skip:** syntax basics.
**Focus:** `context` cancellation, error wrapping with `%w` and `errors.Is/As`, interfaces for
testability, table-driven tests, `net/http` timeouts.

- https://go.dev/blog/context
- https://go.dev/blog/go1.13-errors
- https://quii.gitbook.io/learn-go-with-tests — especially the mocking and context chapters

**Prove it:** write an HTTP client that respects a context deadline and a test that proves a slow
server does not hang it.

### A2. Kubernetes API machinery

**Why:** an operator is an API client with strong opinions. Most operator bugs are API-machinery
misunderstandings.
**Skip:** using the API as an operator.
**Focus:** GVK/GVR, `resourceVersion` and optimistic concurrency (conflict errors), `generation` vs
`observedGeneration`, informers and the cache, owner references and garbage collection, finalizers,
server-side apply.

- https://kubernetes.io/docs/reference/using-api/api-concepts/
- https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md
- https://kubernetes.io/docs/concepts/architecture/garbage-collection/
- https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers/
- https://kubernetes.io/docs/reference/using-api/server-side-apply/
- Book: *Programming Kubernetes*, Hausenblas & Schimanski (O'Reilly)

**Prove it:** explain to a colleague why a status update can fail with a conflict and why the right
response is to requeue, not to retry in a loop.

### A3. kubebuilder and controller-runtime

**Why:** the platform operator.
**Focus:** the reconcile contract, `Owns` vs `Watches`, `EnqueueRequestsFromMapFunc`, predicates,
envtest, webhooks.

- https://book.kubebuilder.io/ — do the CronJob tutorial *after* this course; it will read very differently
- https://book.kubebuilder.io/reference/markers
- https://book.kubebuilder.io/reference/envtest
- https://pkg.go.dev/sigs.k8s.io/controller-runtime
- Book: *Kubernetes Operators*, Dobies & Wood (O'Reilly)

**Prove it:** stage 12.

### A4. Helm, OCI and ORAS

**Why:** every module ships as a chart in an OCI registry, next to its images and metadata.
**Focus:** chart authoring, `values.schema.json`, OCI manifests and media types, digests.

- https://helm.sh/docs/chart_best_practices/
- https://helm.sh/docs/topics/registries/
- https://github.com/opencontainers/image-spec/blob/main/manifest.md
- https://oras.land/docs/

**Prove it:** stage 05, plus: explain what an `artifactType` is and why it lets one registry hold
charts, images and our metadata.

### A5. Harbor, as a product builder

**Why:** distribution and a licensing enforcement point.
**Skip:** installation and day-2 admin — you have done that.
**Focus:** robot accounts via API, project-level permissions, tag immutability, replication for
air-gapped sites, signature verification.

- https://goharbor.io/docs/ — *Working with Projects → Robot Accounts*, *Replication*
- your Harbor's `/devcenter-api-2.0` API explorer

**Prove it:** stage 06, plus: sketch how a licence issuing tool would create, rotate and expire a
customer robot automatically.

### A6. Flux

**Why:** the deployment engine the operator drives.
**Focus:** `OCIRepository`, `HelmRelease`, remediation, drift detection, status conditions, and the
API types for Go.

- https://fluxcd.io/flux/components/source/ocirepositories/
- https://fluxcd.io/flux/components/helm/helmreleases/
- https://fluxcd.io/flux/cheatsheets/oci-artifacts/

**Prove it:** stage 07.

### A7. Signatures, for licensing

**Why:** the licence system.
**Focus:** signing vs encryption, Ed25519, why algorithms get pinned, JWS and PASETO structure, key
rotation with key ids.

- https://pkg.go.dev/crypto/ed25519
- https://www.rfc-editor.org/rfc/rfc8037 — EdDSA in JOSE
- https://paseto.io/
- Book: *Real-World Cryptography*, David Wong — chapters on signatures

**Prove it:** stage 08, plus: explain the JWT `alg` confusion attack and how pinning prevents it.

---

## Phase B — Identity and security

### B1. OAuth 2 and OIDC, as a builder

**Why:** every request carries a token; the CLI logs in with device flow.
**Skip:** Keycloak administration.
**Focus:** Authorization Code + PKCE, Device Authorization Grant, token claims design, refresh,
JWKS rotation.

- https://www.oauth.com/
- https://www.rfc-editor.org/rfc/rfc8628 — device grant
- https://www.keycloak.org/documentation — client scopes, mappers, themes, admin REST API

**Prove it:** a Go CLI that logs in to a Keycloak realm with device flow and prints the token's
claims.

### B2. Istio security for an application platform

**Skip:** multi-cluster, mTLS setup.
**Focus:** `RequestAuthentication` **and** `AuthorizationPolicy` together — and why the first alone
accepts requests with no token; `CUSTOM` action with an external authorizer.

- https://istio.io/latest/docs/reference/config/security/request_authentication/
- https://istio.io/latest/docs/tasks/security/authorization/authz-custom/

**Prove it:** demonstrate, on kind, a route that wrongly accepts a token-less request, then fix it.

### B3. OPA and Rego

**Why:** all authorization decisions.
**Focus:** Rego, bundles, the Envoy plugin, **partial evaluation** for list filtering, policy
testing.

- https://www.openpolicyagent.org/docs/
- https://play.openpolicyagent.org/
- https://www.openpolicyagent.org/docs/envoy-introduction

**Prove it:** a policy that grants `hello-world:greeting:read` by group membership, with `opa test`
cases for allow and deny.

### B4. OPAL

**Why:** pushing role changes to every OPA in under a second.
**When:** after B3, and after phase 1 of the real project — polling comes first.

- https://docs.opal.ac/

**Prove it:** change a role binding and watch it reach two OPA sidecars without a bundle poll.

---

## Phase C — Contracts and APIs

### C1. JSON Schema 2020-12

**Why:** the module contract is written in it.

- https://json-schema.org/understanding-json-schema

**Prove it:** write `v1alpha1.json` for the hello-world metadata from stage 05 and validate it in Go.

### C2. OpenAPI 3.1 and API design

- https://learn.openapis.org/
- https://www.rfc-editor.org/rfc/rfc9457 — problem details for errors
- https://cloud.google.com/apis/design

**Prove it:** the Platform API skeleton, with generated Go client.

### C3. CLI design

- https://clig.dev/
- https://cobra.dev/

**Prove it:** `bssconnectsctl module validate`.

---

## Phase D — Shared services and operations

| Topic | Why | Start |
|---|---|---|
| CloudNativePG | per-module databases | https://cloudnative-pg.io/documentation/ |
| Strimzi | per-module topics and users | https://strimzi.io/documentation/ |
| OpenTelemetry in Go | tracing across modules | https://opentelemetry.io/docs/languages/go/ |
| SLOs and error budgets | reliability as a product feature | https://sre.google/workbook/implementing-slos/ |

You already run Prometheus, Alertmanager and Falco — the new part is **generating** their
configuration per module from the operator.

---

## Phase E — Frontend awareness (enough to review, not to build)

| Topic | Start |
|---|---|
| React + TypeScript basics | https://react.dev/learn |
| Module Federation | https://module-federation.io/ |
| JSON-Schema-driven forms | https://jsonforms.io/ |

---

## Suggested order, at a glance

```
now ──► A1 A2 ──► A3 (course 00–04) ──► A4 A5 (course 05–06) ──► A6 (07) ──► A7 (08) ──► course 09–12
                                                                                              │
     ┌────────────────────────────────────────────────────────────────────────────────────────┘
     ▼
    C1 (the contract) ──► B1 ──► B2 ──► B3 ──► C2 C3 ──► D ──► B4 ──► E
```

C1 comes straight after the course deliberately: the module contract is the first real project
deliverable, and you will have just built something that consumes it.
