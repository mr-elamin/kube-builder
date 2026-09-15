# Operator training — managing the lifecycle of `hello-world`

A hands-on path from an empty kubebuilder scaffold to an operator that installs a licensed
module from Harbor through Flux. **You type every command yourself.** Nothing here is meant to be
copy-pasted in bulk — the understanding comes from typing, breaking, and fixing.

---

## The idea: one module, one concept at a time

You manage **one** module — `hello-world` — through the whole course. The module stays trivial on
purpose; what grows is the operator. Each stage adds exactly one concept, and each stage ends with
something working that you can see in `kubectl`.

```
stage   the operator can...                                    concept you learn
─────   ─────────────────────────────────────────────────────  ─────────────────────────────
 00     (nothing yet)                                          safe local cluster, tooling
 01     (nothing yet)                                          what init generated, and why
 02     create a Deployment from a ModuleInstance              the reconcile loop, owner refs
 03     report whether it is working                           status, conditions, envtest
 04     clean up things owner refs cannot reach                finalizers
 05     (the module becomes a Helm chart in an OCI registry)   Helm, OCI artifacts
 06     (the chart moves to Harbor, pulled with a robot)       Harbor projects, robot accounts
 07     install via Flux instead of by hand                    HelmRelease, deleting your own code
 08     refuse to install without a valid licence              a second CRD, cross-resource watches, signatures
 09     reject a bad ModuleInstance at kubectl apply time      admission webhooks, cert-manager
 10     learn what the module offers once it is running        fetching __metadata__
 11     prove all of the above keeps working                   envtest and e2e in depth
 12     the full lifecycle, end to end                         capstone
```

## Your questions, answered

**"Do we deploy only hello-world?"** Yes, for the whole course. A realistic module would make it
hard to tell whether a failure is in your operator or in the module. `hello-world` removes that
ambiguity.

**"Shall I learn only this first?"** Learn the **operator** first, with hello-world as the subject.
Keycloak, Istio and OPA come later — see [learning-roadmap.md](learning-roadmap.md). An operator
that installs modules is the backbone everything else hangs off; the others are easier to learn once
you have something to plug them into.

**"What about the licence? It is part of the lifecycle."** It is — and it comes at stage 08, not
stage 02, for a concrete reason: **a licence gates a lifecycle, and you cannot gate a lifecycle
that does not exist yet.** By stage 08 you will have install, status, cleanup and upgrade working,
so adding "…but only if licensed" is one clear change with an obvious effect. Adding it on day one
means debugging signatures and reconciliation at the same time.

**"Start simple and gradually add things?"** Exactly. And at stage 07 you will **delete** most of
the Deployment code you wrote in stages 02–04, replacing it with a Flux `HelmRelease`. That is
deliberate: having written rollout logic by hand once, you will understand precisely what Flux is
doing for you, and why the real project never reimplements Helm.

---

## How to work through it

1. **One stage at a time.** Do not read ahead and skim. Each stage assumes the previous one works.
2. **Commit at the end of every stage:** `git commit -am "stage 03: status and conditions"`. When
   something breaks later, `git diff` against a known-good stage tells you what you changed.
3. **Run `git diff` after every kubebuilder command.** Seeing exactly what `create api` or
   `make manifests` wrote teaches the tool faster than reading about it.
4. **Do the "Break it" exercises.** They are the most valuable part. You learn an operator by
   watching how it fails.
5. **Write notes in `training/notes/`** — one file per stage. The questions you had and what
   answered them. This becomes onboarding material for your colleagues.

## Time

With your background (CKA/CKS, Go, you have written CRDs and webhooks before), roughly:

| Stages | Effort |
|---|---|
| 00–04 | 1 week |
| 05–07 | 1 week |
| 08–10 | 1–2 weeks |
| 11–12 | 1 week |

Part-time, double it. Going slower and doing the break-it exercises is worth more than finishing.

## The rule for the whole course

**Never run `make install` or `make deploy` without checking your context first:**

```bash
kubectl config current-context    # must be kind-kb-practice
```

`make install` applies CRDs to whatever cluster you are pointed at. Make that check a reflex.

## Files

| File | |
|---|---|
| [learning-roadmap.md](learning-roadmap.md) | everything to learn for the platform project, in order, with links |
| [00-environment.md](00-environment.md) … [12-capstone.md](12-capstone.md) | the stages |
