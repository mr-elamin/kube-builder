# Stage 02 — Your first API and the reconcile loop

**Goal:** `kubectl apply` a `ModuleInstance` → your operator creates a Deployment running
hello-world. Delete the Deployment → it comes back.
**Concepts:** `create api`, markers, the reconcile loop, `CreateOrUpdate`, owner references,
level-triggered design.

For this stage the module is a public image, `hashicorp/http-echo`, so there is nothing to build.
Stage 05 replaces it with your own.

---

## 1. Create the API

```bash
kubebuilder create api --group platform --version v1alpha1 --kind ModuleInstance --resource --controller
git status
```

Before writing any code, look at what appeared:

```
api/platform/v1alpha1/moduleinstance_types.go    ← your types
internal/controller/platform/moduleinstance_controller.go
internal/controller/platform/suite_test.go       ← envtest harness, stage 03
config/crd/                                      ← empty until make manifests
config/samples/platform_v1alpha1_moduleinstance.yaml
cmd/main.go                                      ← modified: scheme + controller registration
PROJECT                                          ← modified: records the new API
```

`git diff cmd/main.go` — see the two things kubebuilder added. You will add similar lines by hand
for Flux in stage 07.

## 2. Define the spec

In `api/platform/v1alpha1/moduleinstance_types.go`, replace the example `Foo` field:

```go
type ModuleInstanceSpec struct {
	// Module is the module id. Matches the id the module declares in its __metadata__.
	// +kubebuilder:validation:Pattern=`^[a-z][a-z0-9-]{1,30}$`
	Module string `json:"module"`

	// Version is informational in this stage. From stage 05 it selects the chart version.
	// +kubebuilder:validation:Pattern=`^[0-9]+\.[0-9]+\.[0-9]+$`
	Version string `json:"version"`

	// Replicas — capped at 5 so a typo cannot schedule 500 pods on your laptop.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=5
	// +kubebuilder:default=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Message is what hello-world answers with.
	// +kubebuilder:default="hello, world"
	// +optional
	Message string `json:"message,omitempty"`
}
```

Why `*int32` and not `int32`? With a plain `int32`, "not set" and "set to 0" are the same value,
so a user could never scale to zero on purpose. A pointer distinguishes them.

```bash
make manifests generate
git diff config/crd/          # find your Pattern, Minimum, Maximum and default in the YAML
```

## 3. Write the reconciler

In `internal/controller/platform/moduleinstance_controller.go`. Add the RBAC markers next to the
existing ones above `Reconcile`:

```go
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
```

Then the body:

```go
func (r *ModuleInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// 1. Read the object. It may have been deleted since the event was queued — that is normal,
	//    not an error. Returning an error here would retry forever for an object that is gone.
	var mi platformv1alpha1.ModuleInstance
	if err := r.Get(ctx, req.NamespacedName, &mi); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 2. Declare what SHOULD exist. CreateOrUpdate fetches the Deployment (or starts empty), runs
	//    the mutate function, and only calls the API if something actually differs.
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: mi.Name, Namespace: mi.Namespace}}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, dep, func() error {
		labels := map[string]string{
			"app.kubernetes.io/name":     mi.Spec.Module,
			"app.kubernetes.io/instance": mi.Name,
		}
		dep.Spec.Replicas = mi.Spec.Replicas
		dep.Spec.Selector = &metav1.LabelSelector{MatchLabels: labels} // immutable after create; same value each time is fine
		dep.Spec.Template.Labels = labels
		dep.Spec.Template.Spec.Containers = []corev1.Container{{
			Name:  "hello",
			Image: "hashicorp/http-echo:1.0",
			Args:  []string{"-listen=:8080", "-text=" + mi.Spec.Message},
			Ports: []corev1.ContainerPort{{ContainerPort: 8080}},
		}}

		// 3. Ownership. Deleting the ModuleInstance now garbage-collects the Deployment, and any
		//    change to the Deployment wakes this controller up (see SetupWithManager).
		return controllerutil.SetControllerReference(&mi, dep, r.Scheme)
	})
	if err != nil {
		return ctrl.Result{}, err // returning an error = requeue with exponential backoff
	}

	log.Info("reconciled", "deployment", dep.Name, "operation", op)
	return ctrl.Result{}, nil
}

func (r *ModuleInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.ModuleInstance{}).
		Owns(&appsv1.Deployment{}). // reconcile the OWNER when an owned Deployment changes
		Named("platform-moduleinstance").
		Complete(r)
}
```

Imports you will need: `appsv1 "k8s.io/api/apps/v1"`, `corev1 "k8s.io/api/core/v1"`,
`metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"`, `"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"`.
Let your editor's gopls add them.

## 4. Run it

```bash
kubectl config current-context        # kind-kb-practice
make manifests generate install       # install = apply the CRD to the cluster
make run                              # terminal 1 — leave it running
```

Edit `config/samples/platform_v1alpha1_moduleinstance.yaml`:

```yaml
apiVersion: platform.bssconnects.io/v1alpha1
kind: ModuleInstance
metadata:
  name: hello
spec:
  module: hello-world
  version: 0.1.0
  message: "hi from my first operator"
```

```bash
# terminal 2
kubectl apply -f config/samples/platform_v1alpha1_moduleinstance.yaml
kubectl get moduleinstance,deploy,pods
kubectl get deploy hello -o jsonpath='{.metadata.ownerReferences}' | jq
kubectl port-forward deploy/hello 8080:8080 &
curl localhost:8080
```

**Your turn:** add a `Service` for the Deployment the same way — `CreateOrUpdate`, owner reference,
`Owns(&corev1.Service{})`, and the RBAC marker for `services`. Then `curl` through the Service.

## 5. Break it — this is where the learning is

Do each one, predict the result *before* running it, and write down what actually happened.

| # | Do this | Question to answer |
|---|---|---|
| 1 | `kubectl delete deploy hello` | Why does it come back within a second? Which line causes that? |
| 2 | `kubectl scale deploy hello --replicas=3` | Why is it reverted? Is that good or bad for users who scale by hand? |
| 3 | `kubectl patch moduleinstance hello --type=merge -p '{"spec":{"message":"changed"}}'` | Watch the pods roll. Who triggered the rollout — you or Kubernetes? |
| 4 | `kubectl patch moduleinstance hello --type=merge -p '{"spec":{"replicas":9}}'` | Who rejected it, and why did your controller never see it? |
| 5 | `Ctrl-C` the operator. `kubectl delete deploy hello`. Wait. `make run` again. | The delete event happened while nothing was listening. Why does it still get fixed? |
| 6 | Watch the `operation` field in the logs across several reconciles with no changes | Is it `unchanged` every time? If it says `updated` repeatedly, why? |
| 7 | `kubectl delete moduleinstance hello` | Who deleted the Deployment — your code, or something else? |

**Number 5 is the most important idea in this course.** Your reconciler is **level-triggered**: it
looks at the current state and fixes it, instead of reacting to "a delete happened". Events get
missed — operators restart, watches disconnect — and a design that depends on seeing every event is
wrong the first time one is lost. Our real platform's operator must be built exactly this way.

**Number 6:** if you see `updated` on every reconcile, your mutate function is overwriting fields the
API server defaulted (such as `imagePullPolicy` or `terminationMessagePath`), so every comparison
finds a difference. It works, but it is a write on every loop. The long-term fix is **Server-Side
Apply** — note it; you will meet it again.

## Done when

- [ ] applying the sample produces a running Deployment and Service you can `curl`
- [ ] all seven break-it results written in `notes/02.md`, in your own words
- [ ] you can explain level-triggered vs edge-triggered, and why it matters
- [ ] `git commit -am "stage 02: first reconciler"`
