# Stage 03 — Status, conditions, and your first test

**Goal:** `kubectl get moduleinstance` shows whether hello-world is actually working, and
`kubectl wait --for=condition=Ready` works.
**Concepts:** the status subresource, `metav1.Condition`, `observedGeneration`, printer columns,
envtest.

Right now your operator creates things and stays silent. A user — or the UI and CLI in our real
platform — has no idea whether the module is healthy. **Status is the only output a human reads.**

---

## 1. Spec vs status

| | Written by | Means |
|---|---|---|
| `spec` | the user | what I want |
| `status` | the controller | what I observed |

They are **separate endpoints** on the API server (that is what `+kubebuilder:subresource:status`
does). A user updating `spec` cannot overwrite `status`, and your controller updating `status`
does not bump `metadata.generation`.

## 2. Define the status

Your scaffold may already contain a `Conditions` field — keep it if so.

```go
type ModuleInstanceStatus struct {
	// ObservedGeneration is the metadata.generation this status was computed from.
	// If it is lower than metadata.generation, the status is stale: the user changed the spec
	// and the controller has not caught up yet. Clients must check this before trusting status.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	AvailableReplicas int32 `json:"availableReplicas,omitempty"`

	// The standard Kubernetes condition list. listType=map keyed on type means one entry per
	// condition type, and server-side apply can merge individual conditions correctly.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

Add printer columns on the `ModuleInstance` type:

```go
// +kubebuilder:printcolumn:name="Module",type=string,JSONPath=`.spec.module`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
```

## 3. Compute and write status

At the end of `Reconcile`, after `CreateOrUpdate`:

```go
	desired := int32(1)
	if dep.Spec.Replicas != nil {
		desired = *dep.Spec.Replicas
	}

	// A Deployment is "done" only when its OWN controller has observed the latest spec AND the
	// replicas are updated and available. Checking AvailableReplicas alone reports Ready while an
	// old ReplicaSet is still serving during a rollout.
	ready := dep.Status.ObservedGeneration >= dep.Generation &&
		dep.Status.UpdatedReplicas == desired &&
		dep.Status.AvailableReplicas == desired

	cond := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: mi.Generation,
	}
	if ready {
		cond.Status, cond.Reason = metav1.ConditionTrue, "DeploymentAvailable"
		cond.Message = fmt.Sprintf("%d/%d replicas available", dep.Status.AvailableReplicas, desired)
	} else {
		cond.Status, cond.Reason = metav1.ConditionFalse, "Progressing"
		cond.Message = fmt.Sprintf("%d/%d replicas available", dep.Status.AvailableReplicas, desired)
	}

	// SetStatusCondition only changes lastTransitionTime when Status actually flips — which is
	// what lets users see "Ready since 14:02" rather than a timestamp that moves every reconcile.
	meta.SetStatusCondition(&mi.Status.Conditions, cond)
	mi.Status.ObservedGeneration = mi.Generation
	mi.Status.AvailableReplicas = dep.Status.AvailableReplicas

	// Status().Update, NOT Update. A plain Update would try to write spec and silently drop status.
	// A conflict error here means someone changed the object since we read it — returning the
	// error requeues, and the next attempt reads fresh data. Never "retry in a loop" yourself.
	if err := r.Status().Update(ctx, &mi); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
```

Import `"k8s.io/apimachinery/pkg/api/meta"` and `"fmt"`.

Notice there is **no polling**. When the Deployment's pods become available, the Deployment's status
changes, `Owns()` delivers that event, and your reconcile runs again. You never wrote "check every
10 seconds".

```bash
make manifests generate install
make run
kubectl apply -f config/samples/platform_v1alpha1_moduleinstance.yaml
kubectl get moduleinstance -w                       # watch Ready flip False → True
kubectl wait --for=condition=Ready moduleinstance/hello --timeout=60s
```

## 4. Break it

| # | Do this | Question |
|---|---|---|
| 1 | Change the image in your code to `hashicorp/http-echo:does-not-exist`, restart | What do Ready and Reason show? Is "Progressing" honest? What reason *should* it show? |
| 2 | Set `replicas: 0` | Is zero-of-zero "Ready"? Should it be? |
| 3 | Change `cond.Reason` to `"Deployment Available"` (with a space) | Who rejects it, and what does the error say? |
| 4 | Change `r.Status().Update` to `r.Update` | What happens to status, and why is there no error? |
| 5 | Patch the spec and immediately `kubectl get moduleinstance hello -o yaml` | Catch a moment where `status.observedGeneration < metadata.generation`. What does that mean to a client? |

**Exercise from #1:** improve it. Detect `ImagePullBackOff` from the pods and report
`Reason: ImagePullFailed` with the image name in the message. A status that says "Progressing"
forever while the image cannot be pulled is the kind of thing that wastes an hour of someone's day.

## 5. Your first envtest

`internal/controller/platform/suite_test.go` starts a **real API server and etcd** in-process. No
kubelet, no scheduler, **no built-in controllers**. That last point is the classic trap:

> In envtest, nobody runs the Deployment controller. Your Deployment will **never** get pods, and
> `status.availableReplicas` will stay 0 forever. To test "becomes Ready", your test must **set the
> Deployment's status itself**, playing the role of Kubernetes.

In `moduleinstance_controller_test.go`, add a test:

```go
It("reports Ready once the Deployment is available", func() {
	ctx := context.Background()
	mi := &platformv1alpha1.ModuleInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "t1", Namespace: "default"},
		Spec:       platformv1alpha1.ModuleInstanceSpec{Module: "hello-world", Version: "0.1.0"},
	}
	Expect(k8sClient.Create(ctx, mi)).To(Succeed())

	r := &ModuleInstanceReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
	req := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(mi)}

	_, err := r.Reconcile(ctx, req)
	Expect(err).NotTo(HaveOccurred())

	// Pretend to be the Deployment controller.
	var dep appsv1.Deployment
	Expect(k8sClient.Get(ctx, req.NamespacedName, &dep)).To(Succeed())
	dep.Status.ObservedGeneration = dep.Generation
	dep.Status.Replicas, dep.Status.UpdatedReplicas, dep.Status.AvailableReplicas = 1, 1, 1
	Expect(k8sClient.Status().Update(ctx, &dep)).To(Succeed())

	_, err = r.Reconcile(ctx, req)
	Expect(err).NotTo(HaveOccurred())

	Expect(k8sClient.Get(ctx, req.NamespacedName, mi)).To(Succeed())
	ready := meta.FindStatusCondition(mi.Status.Conditions, "Ready")
	Expect(ready).NotTo(BeNil())
	Expect(ready.Status).To(Equal(metav1.ConditionTrue))
})
```

```bash
make test
```

From here on, **every stage ends with at least one new test.**

## Done when

- [ ] `kubectl get moduleinstance` shows Module, Version, Ready, Reason
- [ ] `kubectl wait --for=condition=Ready` works
- [ ] bad image shows a specific, honest reason
- [ ] `make test` passes with your new test
- [ ] `git commit -am "stage 03: status, conditions, first envtest"`
