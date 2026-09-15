# Stage 04 — Finalizers: cleaning up what owner references cannot reach

**Goal:** each module runs in its own namespace, `module-<name>`, and deleting the
`ModuleInstance` removes that namespace.
**Concepts:** owner-reference limits, finalizers, deletion timestamps, label-based watches.

This is not an invented exercise. **Our real platform does exactly this** — every module gets its
own namespace — and it runs straight into the limitation below.

---

## 1. Why owner references are not enough

Garbage collection via owner references has two hard rules:

1. A namespaced owner can only own objects **in the same namespace**.
2. A namespaced owner can **never** own a cluster-scoped object — and `Namespace` is cluster-scoped.

Your `ModuleInstance` lives in `default`. The namespace `module-hello` is cluster-scoped. The
Deployment will live *inside* `module-hello`. Both rules are broken.

**Try it first** — this is the fastest way to believe it. Temporarily change the Deployment's
namespace to `"module-" + mi.Name`, keep `SetControllerReference`, and run. Read the error.

## 2. What a finalizer is

A finalizer is just a string in `metadata.finalizers`. While that list is non-empty, a delete request
**does not delete the object** — it only sets `metadata.deletionTimestamp`. The object stays visible,
in a "terminating" state, until every finalizer is removed.

That gives your controller a window: see the deletion timestamp, clean up external things, remove
your finalizer, and let the deletion finish.

```
kubectl delete moduleinstance hello
        │
        ▼
deletionTimestamp set ──► your Reconcile runs ──► delete namespace ──► remove finalizer ──► object gone
                                    │
                                    └── operator down? the object waits, visibly, until it comes back
```

## 3. Implement it

```go
const (
	cleanupFinalizer       = "platform.bssconnects.io/cleanup"
	labelInstanceName      = "platform.bssconnects.io/instance-name"
	labelInstanceNamespace = "platform.bssconnects.io/instance-namespace"
)

func moduleNamespace(mi *platformv1alpha1.ModuleInstance) string { return "module-" + mi.Name }
```

RBAC — namespaces are cluster-scoped, so this only works because your manager is cluster-scoped
(the reason we did **not** pass `--namespaced` to `init`):

```go
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;update;patch;delete
```

At the top of `Reconcile`, right after the `Get`:

```go
	// Being deleted?
	if !mi.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&mi, cleanupFinalizer) {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: moduleNamespace(&mi)}}
			// Already gone is success. Anything else: return the error and try again later —
			// removing the finalizer after a FAILED cleanup would orphan the namespace forever.
			if err := r.Delete(ctx, ns); client.IgnoreNotFound(err) != nil {
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(&mi, cleanupFinalizer)
			if err := r.Update(ctx, &mi); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil // nothing else to do for an object being deleted
	}

	// Not being deleted: make sure our finalizer is present BEFORE creating anything it must
	// clean up. The other order has a window where a namespace exists that nothing will delete.
	if controllerutil.AddFinalizer(&mi, cleanupFinalizer) {
		if err := r.Update(ctx, &mi); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Create the module namespace.
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: moduleNamespace(&mi)}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, ns, func() error {
		if ns.Labels == nil {
			ns.Labels = map[string]string{}
		}
		ns.Labels[labelInstanceName] = mi.Name
		ns.Labels[labelInstanceNamespace] = mi.Namespace
		return nil
	}); err != nil {
		return ctrl.Result{}, err
	}
```

Move the Deployment (and Service) into `moduleNamespace(&mi)`. **Remove** `SetControllerReference`
from their mutate functions, and add the two labels instead — labels are how you will find your way
back from a Deployment to its `ModuleInstance`.

## 4. Replace `Owns()` with a label-based watch

`Owns()` works by following owner references, which you no longer have. So map events yourself:

```go
func (r *ModuleInstanceReconciler) instanceForObject(_ context.Context, obj client.Object) []reconcile.Request {
	l := obj.GetLabels()
	name, ns := l[labelInstanceName], l[labelInstanceNamespace]
	if name == "" || ns == "" {
		return nil // not one of ours
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: name, Namespace: ns}}}
}

func (r *ModuleInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.ModuleInstance{}).
		Watches(&appsv1.Deployment{}, handler.EnqueueRequestsFromMapFunc(r.instanceForObject)).
		Watches(&corev1.Service{}, handler.EnqueueRequestsFromMapFunc(r.instanceForObject)).
		Named("platform-moduleinstance").
		Complete(r)
}
```

`EnqueueRequestsFromMapFunc` is one of the most useful tools in controller-runtime. You will use it
again at stage 08 to wake up every `ModuleInstance` when a `License` changes.

## 5. Break it

| # | Do this | Question |
|---|---|---|
| 1 | `kubectl delete moduleinstance hello` | Watch `kubectl get ns -w`. What order do things happen in? |
| 2 | Stop the operator. `kubectl delete moduleinstance hello`. | The command hangs or returns, but the object stays. What does `kubectl get moduleinstance hello -o yaml` show? Start the operator — what happens? |
| 3 | With the operator stopped and the object terminating: `kubectl patch moduleinstance hello --type=merge -p '{"metadata":{"finalizers":null}}'` | The object disappears. Is `module-hello` still there? Who will ever delete it? |
| 4 | Delete the CRD while instances exist: `make uninstall` | What happens to terminating instances when their controller and their type both vanish? |
| 5 | Delete `module-hello` by hand while the instance exists | Does it come back? Which watch caused that? |

**Number 3 is a real operations lesson.** Stripping finalizers is the standard "unstick a
terminating object" trick you will find on every forum, and it silently leaks whatever the finalizer
was protecting. When someone suggests it in production, the right question is: *what was this
finalizer cleaning up, and who will do that now?*

**Design question for `notes/04.md`:** namespace deletion is itself asynchronous — the namespace
goes `Terminating` while Kubernetes deletes everything in it. Your finalizer removes itself as soon
as the delete is *requested*. Should it instead wait until the namespace is fully gone? What would
you gain, and what could go wrong? (Try implementing the waiting version with `RequeueAfter`.)

## Done when

- [ ] `hello-world` runs in `module-hello`
- [ ] deleting the instance removes the namespace
- [ ] the instance survives an operator restart mid-deletion and completes
- [ ] a test checks the finalizer is added on first reconcile
- [ ] `git commit -am "stage 04: module namespaces and finalizers"`
