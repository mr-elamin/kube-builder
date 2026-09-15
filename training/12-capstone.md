# Stage 12 — Capstone: the full lifecycle, from nothing

**Goal:** prove to yourself — and then to a colleague — that you understand every moving part.

---

## 1. From a clean machine

Delete everything and rebuild without looking at earlier stages. Where you get stuck, that is the
part to review.

```bash
kind delete cluster --name kb-practice
# recreate cluster + registry (stage 00)
# install Flux, cert-manager
# build and push hello-world 0.1.0 and 0.2.0 to Harbor with ci-pusher
# generate a fresh signing key
# build and deploy the operator in-cluster, with the new public key
```

## 2. The scenario

Run it end to end, and **predict each status before you look**:

| Step | Action | Expect |
|---|---|---|
| 1 | apply `ModuleInstance` hello 0.1.0, no licence | webhook **warning**; `Licensed=False`; nothing installed |
| 2 | apply a licence for `>=0.1.0 <1.0.0`, valid 10 minutes | `Licensed=True` → HelmRelease → `Ready=True` → `metadataRevision` set |
| 3 | `curl` the module | your message, v0.1.0 |
| 4 | patch version to `0.2.0` | Flux upgrades; metadata fetched again for 0.2.0 |
| 5 | patch version to `1.0.0` | `Licensed=False`; 0.2.0 **keeps running** |
| 6 | patch version to `01.0.0` | **rejected** by the webhook |
| 7 | patch `module` to `crm` | **rejected** by the webhook |
| 8 | wait for the licence to expire | `Licensed` flips on its own; nothing uninstalled |
| 9 | apply a renewed licence | `Licensed=True` again, no other action needed |
| 10 | stop the operator; delete the instance; start the operator | deletion completes; namespace removed |

If any step surprises you, find out why before moving on.

## 3. Map it to the real platform — `notes/12.md`

For each thing you built, name the real platform component it corresponds to and one thing the real
one must do that yours does not:

| You built | Real platform | The real one must also… |
|---|---|---|
| `ModuleInstance` controller | platform operator | … |
| `License` + controller | License Service + `License` CRD | … |
| `licence-tool` | offline signing tooling | … |
| `Entitled()` | install-time gate + runtime `pkg/license` SDK | … |
| `harbor-puller` secret | per-customer robot derived from the licence | … |
| metadata ConfigMap | Module Registry | … |
| validating webhook | admission webhook validating config against `values.schema.json` | … |

Fill in the last column yourself. That column is effectively the design for the real operator.

## 4. Teach it back

Give a 30-minute session to a colleague using only your cluster and your `notes/` folder. No slides.

Teaching is the test. The places where you hesitate while explaining are the places you do not yet
understand — and your notes, cleaned up, become onboarding material for the whole team.

## 5. What's next

Your operator now does the core of what the real one does. The things it does **not** yet touch are
the rest of the platform: identity (Keycloak), the gateway and mesh (Istio), authorization (OPA,
OPAL), and the contracts (JSON Schema, OpenAPI). See [learning-roadmap.md](learning-roadmap.md),
Phase B onward.
