# Stage 09 — Admission webhooks: stop mistakes at `kubectl apply`

**Goal:** a malformed `ModuleInstance` is rejected the moment it is applied, and an unlicensed one is
accepted with a **warning**.
**Concepts:** validating webhooks, warnings vs errors, cert-manager, running in-cluster, and the
day `make run` stops being enough.

---

## 1. Why webhooks, when markers already validate?

Markers produce OpenAPI validation: types, patterns, ranges on **one object in isolation**. They
cannot express rules that need logic or **other objects**:

| Rule | Markers? | Webhook? |
|---|---|---|
| `version` looks like `x.y.z` | ✅ Pattern | |
| `version` is valid **semver** (no leading zeros, prerelease rules) | ❌ | ✅ |
| `module` cannot change after creation | ❌ (CEL can, actually — look it up) | ✅ |
| a licence exists for this module | ❌ | ✅ — reads other objects |

Prefer markers and CEL (`+kubebuilder:validation:XValidation`) wherever they are enough: they run
inside the API server and cannot be down. A webhook is **another service your cluster depends on**.

## 2. Errors vs warnings — the design decision

Should an unlicensed `ModuleInstance` be **rejected**? It is tempting. Consider GitOps: a customer
commits the `License` and the `ModuleInstance` in the same change. Their tool applies them in some
order. If the instance lands first, a rejecting webhook fails the whole sync — and the customer sees
a platform that cannot even be installed from Git.

So:

- **Reject** what is **wrong in itself** — invalid semver, changing `module` after creation.
- **Warn** about what is **not true yet** — no matching licence *right now*.

The controller already handles "not licensed yet" correctly with a condition. The webhook's job is
to give fast feedback, not to enforce ordering. This is the principle *"no ordering assumptions"*
from the platform design, met in practice.

## 3. Scaffold the webhook

```bash
kubebuilder create webhook --group platform --version v1alpha1 --kind ModuleInstance --programmatic-validation
git status
```

Find the generated file under `internal/webhook/platform/v1alpha1/`. Keep the structure and method
signatures kubebuilder generated — they vary slightly between controller-runtime versions. Add a
client to the validator, and pass `mgr.GetClient()` where it is constructed in the setup function.

```go
type ModuleInstanceCustomValidator struct {
	Client client.Client
}

func (v *ModuleInstanceCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	mi, ok := obj.(*platformv1alpha1.ModuleInstance)
	if !ok {
		return nil, fmt.Errorf("expected ModuleInstance, got %T", obj)
	}

	// Wrong in itself → reject.
	if _, err := semver.StrictNewVersion(mi.Spec.Version); err != nil {
		return nil, field.Invalid(field.NewPath("spec", "version"), mi.Spec.Version,
			"must be strict semantic version, e.g. 1.2.3")
	}

	// Not true yet → warn, never reject. See the GitOps argument above.
	var lics licensingv1alpha1.LicenseList
	if err := v.Client.List(ctx, &lics, client.InNamespace(mi.Namespace)); err != nil {
		// Cannot check? Say so, and let it through. A webhook that fails requests because it
		// could not read a cache makes the whole API unreliable.
		return admission.Warnings{"licence check skipped: " + err.Error()}, nil
	}
	if ok, _ := controllerplatform.Entitled(lics.Items, mi.Spec.Module, mi.Spec.Version, time.Now()); !ok {
		return admission.Warnings{fmt.Sprintf(
			"no valid licence currently covers %s %s — it will install once one is applied",
			mi.Spec.Module, mi.Spec.Version)}, nil
	}
	return nil, nil
}

func (v *ModuleInstanceCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldMI := oldObj.(*platformv1alpha1.ModuleInstance)
	newMI := newObj.(*platformv1alpha1.ModuleInstance)
	if oldMI.Spec.Module != newMI.Spec.Module {
		return nil, field.Forbidden(field.NewPath("spec", "module"),
			"cannot be changed; delete and recreate the ModuleInstance")
	}
	return v.ValidateCreate(ctx, newObj)
}
```

Your stage-08 `Entitled` function is reused here unchanged. That is the payoff of having made it a
pure function instead of burying it inside `Reconcile`.

## 4. Why `make run` stops working

The **API server** calls the webhook, over **HTTPS**, using a certificate it trusts, at a Service
address **inside the cluster**. Your laptop is none of those things. So from this stage on, the
operator runs in the cluster:

```bash
# cert-manager issues the webhook's serving certificate and injects the CA into the webhook config
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.2/cert-manager.yaml
kubectl -n cert-manager rollout status deploy/cert-manager-webhook
```

(Pick the current release from https://github.com/cert-manager/cert-manager/releases.)

Open `config/default/kustomization.yaml`. If the `webhook` and `certmanager` sections and their
replacements are commented out, enable them — read each block before uncommenting.

The operator also needs `LICENSE_PUBLIC_KEY` and the chart registry setting inside the Pod now: add
them to `config/manager/manager.yaml` (the public key is not secret, so an env var is fine).

```bash
kubectl config current-context
export IMG=localhost:5001/nawat-operator:dev
make docker-build docker-push IMG=$IMG
make deploy IMG=$IMG
kubectl -n nawat-system logs deploy/nawat-controller-manager -f
```

## 5. The RBAC surprise

The first thing that probably happens: `forbidden` errors in the logs. With `make run`, the operator
used **your** kubeconfig — cluster-admin. Now it runs as its own ServiceAccount, with **only** the
permissions your `+kubebuilder:rbac` markers generated.

Every missing marker now shows up. Fix each by adding the marker, not by granting cluster-admin.
This is stage 01's question 8 arriving, and it is why the real project's CI must test the operator
**deployed**, not only via `make run`.

## 6. Break it

| # | Do this | Question |
|---|---|---|
| 1 | Apply `version: 01.2.3` | Who rejects it — the marker or the webhook? Why? |
| 2 | Apply an instance with no licence in the namespace | Where does the warning appear in your terminal? |
| 3 | Change `spec.module` on an existing instance | Read the error a user would see. Clear enough? |
| 4 | `kubectl -n nawat-system scale deploy nawat-controller-manager --replicas=0`, then create an instance | What happens? Check `failurePolicy` in the generated webhook configuration. |
| 5 | With the operator still at 0 replicas, try to **delete** an instance | Blocked or not? Why? |
| 6 | Make `ValidateCreate` sleep 15 seconds | What does `kubectl apply` do? What is the webhook's `timeoutSeconds`? |

**Number 4 is the operational lesson.** With `failurePolicy: Fail`, your webhook's availability
becomes the API's availability for that kind. Two replicas, a PodDisruptionBudget, and scoping with
`namespaceSelector`/`objectSelector` are not optional in production.

## Done when

- [ ] invalid semver rejected; module change rejected; unlicensed instance warned, not rejected
- [ ] the operator runs in-cluster with only generated RBAC
- [ ] `notes/09.md` explains why the unlicensed case is a warning
- [ ] `git commit -am "stage 09: validating webhook, in-cluster operator"`
