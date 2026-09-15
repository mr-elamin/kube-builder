# Stage 07 — Let Flux install the module, and delete your own code

**Goal:** the operator stops creating Deployments. It creates an `OCIRepository` and a
`HelmRelease`, and Flux does the installing, upgrading and rolling back.
**Concepts:** Flux sources, `HelmRelease`, importing another project's API types, mirroring status,
and why the real platform never reimplements Helm.

---

## 1. Install Flux, and use it by hand first

Learn the tool before your code drives it.

```bash
kubectl config current-context      # kind-kb-practice
flux check --pre
flux install
flux check
```

Check which API versions your Flux serves — **your Go imports must match these**:

```bash
kubectl api-resources | grep -E 'helmreleases|ocirepositories'
```

Create a namespace, the pull secret from stage 06, and Flux objects by hand:

```bash
kubectl create namespace flux-manual
kubectl -n flux-manual create secret docker-registry harbor-puller \
  --docker-server="$HARBOR" \
  --docker-username='robot$kb-training+cluster-puller' \
  --docker-password="$PULLER_SECRET"
```

```yaml
# flux-manual.yaml
apiVersion: source.toolkit.fluxcd.io/v1        # use the version api-resources showed you
kind: OCIRepository
metadata:
  name: hello-world
  namespace: flux-manual
spec:
  interval: 5m
  url: oci://harbor.your-company.example/kb-training/charts/hello-world
  ref:
    tag: 0.1.0
  secretRef:
    name: harbor-puller
  # A Helm chart artifact has one layer. Tell Flux to take it as-is rather than extract it —
  # compare this media type with the one you wrote down in stage 05.
  layerSelector:
    mediaType: application/vnd.cncf.helm.chart.content.v1.tar+gzip
    operation: copy
---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: hello-world
  namespace: flux-manual
spec:
  interval: 5m
  releaseName: hello-world
  chartRef:
    kind: OCIRepository
    name: hello-world
  values:
    message: "installed by flux"
    image:
      repository: harbor.your-company.example/kb-training/images/hello-world
  upgrade:
    remediation:
      retries: 3
      strategy: rollback      # a failed upgrade rolls itself back
```

The chart's pods also need `imagePullSecrets` for Harbor. Either add that to your chart's values and
template (the better lesson), or pull the image into kind with `kind load docker-image`.

```bash
kubectl apply -f flux-manual.yaml
flux get sources oci -n flux-manual
flux get helmreleases -n flux-manual
kubectl -n flux-manual get hr hello-world -o yaml     # read status.conditions carefully
```

Try: change `ref.tag` to `9.9.9`. Watch how the failure surfaces. Change it back. Then
`kubectl delete ns flux-manual`.

## 2. Now make the operator do it

```bash
cd ~/tmp/kubebuilder
go get github.com/fluxcd/helm-controller/api@latest
go get github.com/fluxcd/source-controller/api@latest
go get github.com/fluxcd/pkg/apis/meta@latest
```

**Register the schemes** in `cmd/main.go`, beside the existing `AddToScheme` lines. Without this,
your client cannot serialise Flux objects and fails with "no kind is registered":

```go
	utilruntime.Must(helmv2.AddToScheme(scheme))
	utilruntime.Must(sourcev1.AddToScheme(scheme))
```

RBAC markers:

```go
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=ocirepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;list;watch;create;update;patch;delete
```

## 3. Delete, then replace

**Delete** the Deployment and Service code from stages 02–04. Keep: the namespace, the finalizer,
the status logic. Before deleting, run `git diff --stat` afterwards and note how many lines went.

Replace with:

```go
import (
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1" // or v1beta2 — match api-resources
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/utils/ptr"
)

	// --- the chart source ---
	repo := &sourcev1.OCIRepository{ObjectMeta: metav1.ObjectMeta{Name: mi.Name, Namespace: mi.Namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, repo, func() error {
		repo.Spec.Interval = metav1.Duration{Duration: 5 * time.Minute}
		repo.Spec.URL = "oci://" + r.ChartRegistry + "/" + mi.Spec.Module
		repo.Spec.Reference = &sourcev1.OCIRepositoryRef{Tag: mi.Spec.Version}
		repo.Spec.SecretRef = &fluxmeta.LocalObjectReference{Name: "harbor-puller"}
		repo.Spec.LayerSelector = &sourcev1.OCILayerSelector{
			MediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
			Operation: sourcev1.OCILayerCopy,
		}
		// Same namespace as the ModuleInstance, so an owner reference is allowed again.
		return controllerutil.SetControllerReference(&mi, repo, r.Scheme)
	}); err != nil {
		return ctrl.Result{}, err
	}

	// --- the release ---
	values, err := json.Marshal(map[string]any{
		"message":      mi.Spec.Message,
		"replicaCount": mi.Spec.Replicas,
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	hr := &helmv2.HelmRelease{ObjectMeta: metav1.ObjectMeta{Name: mi.Name, Namespace: mi.Namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, hr, func() error {
		hr.Spec.Interval = metav1.Duration{Duration: 5 * time.Minute}
		hr.Spec.ReleaseName = mi.Spec.Module
		hr.Spec.TargetNamespace = moduleNamespace(&mi) // the namespace from stage 04
		hr.Spec.ChartRef = &helmv2.CrossNamespaceSourceReference{Kind: "OCIRepository", Name: repo.Name}
		// This one field replaces rollback logic you would otherwise write and get subtly wrong.
		hr.Spec.Upgrade = &helmv2.Upgrade{
			Remediation: &helmv2.UpgradeRemediation{
				Retries:  3,
				Strategy: ptr.To(helmv2.RollbackRemediationStrategy),
			},
		}
		hr.Spec.Values = &apiextensionsv1.JSON{Raw: values}
		return controllerutil.SetControllerReference(&mi, hr, r.Scheme)
	}); err != nil {
		return ctrl.Result{}, err
	}

	// --- mirror Flux's verdict onto OUR status ---
	// Users, the CLI and the UI read ModuleInstance only. They must never need to understand Flux.
	cond := metav1.Condition{Type: "Ready", ObservedGeneration: mi.Generation,
		Status: metav1.ConditionUnknown, Reason: "Installing", Message: "waiting for Flux"}
	if fc := meta.FindStatusCondition(hr.Status.Conditions, "Ready"); fc != nil {
		cond.Status, cond.Message = fc.Status, fc.Message
		cond.Reason = "Flux" + fc.Reason
	}
	meta.SetStatusCondition(&mi.Status.Conditions, cond)
```

Add `ChartRegistry string` to the reconciler struct and set it in `cmd/main.go` from a flag or
environment variable, e.g. `harbor.your-company.example/kb-training/charts`.

The `harbor-puller` secret must exist in the ModuleInstance's namespace. Create it by hand for now;
note in `notes/07.md` which component should really create it, and from what.

Setup:

```go
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.ModuleInstance{}).
		Owns(&sourcev1.OCIRepository{}).
		Owns(&helmv2.HelmRelease{}).        // Flux updates HelmRelease status → we reconcile
		Watches(&corev1.Namespace{}, handler.EnqueueRequestsFromMapFunc(r.instanceForObject)).
		Named("platform-moduleinstance").
		Complete(r)
```

On deletion, the owner references remove the `HelmRelease`, which makes Flux **uninstall the
release**. Your stage-04 finalizer still deletes the namespace. Everything you learned carries over.

## 4. Break it

| # | Do this | Question |
|---|---|---|
| 1 | `version: 9.9.9` | Where does the error appear first — OCIRepository, HelmRelease, or your status? Is your status message good enough? |
| 2 | Push chart `0.2.0` whose readiness probe points at a path that returns 404. Upgrade to it | Watch the rollback happen. How much code did you write for that? |
| 3 | `flux suspend hr hello -n default`, then edit the Deployment in `module-hello` by hand | Nothing reverts it. `flux resume hr hello -n default` — what happens? |
| 4 | `kubectl delete moduleinstance hello` | List, in order, every object that disappears and who deleted each one. |
| 5 | Remove the scheme registration from `main.go` | Read the exact error — you will see it again in a real project. |

## 5. The reflection — `notes/07.md`

Count what you deleted. Then list what Flux now does that your old code did not: rollback, retry
with backoff, drift detection, release history, chart fetching and caching, dependency ordering.

This is the argument, from experience, for the rule in the platform design: **the operator decides
what should exist; Flux decides how it gets applied.** You have now done it both ways.

## Done when

- [ ] the operator creates `OCIRepository` + `HelmRelease`; no Deployment code remains
- [ ] upgrade to a broken version rolls back without your code doing anything
- [ ] `ModuleInstance` Ready reflects Flux's verdict
- [ ] `notes/07.md` has the line count and the list
- [ ] `git commit -am "stage 07: install through Flux"`
