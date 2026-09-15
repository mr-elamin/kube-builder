# Stage 11 — Proving it keeps working

**Goal:** a test suite that would catch every break-it exercise you did, so you never have to
re-check them by hand.
**Concepts:** the test pyramid for operators, pure functions, envtest's limits, Flux CRDs in
envtest, e2e on kind, flakiness.

You have been adding a test per stage since stage 03. This stage makes the suite deliberate.

---

## 1. Three layers

```
                ┌──────────────┐
                │   e2e (kind) │  few · slow · real Flux, real Harbor pulls, real webhooks
                ├──────────────┤
                │   envtest    │  some · seconds · real API server, NO controllers, NO GC
                ├──────────────┤
                │ pure funcs   │  many · milliseconds · no Kubernetes at all
                └──────────────┘
```

**Push logic down the pyramid.** Every decision you could make a pure function — you already have
`licence.Verify`, `Entitled`, `validateMetadata` — should be tested there, exhaustively, in
milliseconds. Keep envtest for "does the reconciler wire things together", and e2e for "does the
whole thing work on a cluster".

## 2. Pure functions: table-driven tests

`internal/controller/platform/entitled_test.go`:

```go
func TestEntitled(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	valid := func(module, rng string, notAfter time.Time) licensingv1alpha1.License {
		return licensingv1alpha1.License{Status: licensingv1alpha1.LicenseStatus{
			Conditions:   []metav1.Condition{{Type: "Valid", Status: metav1.ConditionTrue}},
			Entitlements: []licensingv1alpha1.EntitlementStatus{{Module: module, VersionRange: rng, NotAfter: metav1.NewTime(notAfter)}},
		}}
	}
	tomorrow, yesterday := now.Add(24*time.Hour), now.Add(-24*time.Hour)

	// Every row is a break-it exercise from stage 08 that will now never regress silently.
	cases := []struct {
		name     string
		lics     []licensingv1alpha1.License
		module   string
		version  string
		wantOK   bool
		wantWhy  string
	}{
		{"no licences", nil, "hello-world", "0.1.0", false, "NoEntitlement"},
		{"matching", []licensingv1alpha1.License{valid("hello-world", ">=0.1.0 <1.0.0", tomorrow)}, "hello-world", "0.1.0", true, "Entitled"},
		{"expired", []licensingv1alpha1.License{valid("hello-world", ">=0.1.0 <1.0.0", yesterday)}, "hello-world", "0.1.0", false, "NoEntitlement"},
		{"expires exactly now", []licensingv1alpha1.License{valid("hello-world", ">=0.1.0", now)}, "hello-world", "0.1.0", false, "NoEntitlement"},
		{"version outside range", []licensingv1alpha1.License{valid("hello-world", "<0.2.0", tomorrow)}, "hello-world", "0.2.0", false, "NoEntitlement"},
		{"other module", []licensingv1alpha1.License{valid("crm", ">=0.0.0", tomorrow)}, "hello-world", "0.1.0", false, "NoEntitlement"},
		{"bad version", nil, "hello-world", "banana", false, "InvalidVersion"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, why := Entitled(tc.lics, tc.module, tc.version, now)
			if ok != tc.wantOK || why != tc.wantWhy {
				t.Fatalf("got (%v, %s), want (%v, %s)", ok, why, tc.wantOK, tc.wantWhy)
			}
		})
	}
}
```

Notice `now` is a parameter. **A function that calls `time.Now()` internally cannot be tested at the
boundary.** "Expires exactly now" is only testable because you passed time in.

**Your turn:** the same for `licence.Verify` — valid, one byte flipped in payload, one byte flipped in
signature, wrong key, missing dot, empty string, garbage base64. And for `validateMetadata`.

## 3. envtest: know what is missing

envtest runs a real `kube-apiserver` and `etcd`. It does **not** run:

| Missing | Consequence in your tests |
|---|---|
| kubelet, scheduler | pods never run; Deployments never become available |
| kube-controller-manager | **no garbage collection** — deleting an owner never deletes children |
| namespace controller | a deleted namespace stays `Terminating` forever |
| Flux | HelmReleases never get a status |
| cert-manager, webhooks | not called unless you start them explicitly |

So in envtest you test **what your controller does**, and you **play the role of every other
controller** by writing their status yourself (as in stage 03).

**Testing deletion correctly:** do not assert "the HelmRelease is gone" — without GC it never will
be. Assert "the HelmRelease has an owner reference to the ModuleInstance". That is your code's
responsibility; the deletion is Kubernetes'.

**Flux types in envtest:** the API server must know the Flux CRDs, or creating a `HelmRelease` fails.
Download them once into `test/crds/flux/` and add that path to `CRDDirectoryPaths` in
`suite_test.go`:

```bash
mkdir -p test/crds/flux
flux install --export > /tmp/flux.yaml
# keep only the CustomResourceDefinition documents — yq or a text editor both work
```

```go
testEnv = &envtest.Environment{
	CRDDirectoryPaths: []string{
		filepath.Join("..", "..", "..", "config", "crd", "bases"),
		filepath.Join("..", "..", "..", "test", "crds", "flux"),
	},
	ErrorIfCRDPathMissing: true,
}
```

And register the Flux schemes in the test's scheme, as you did in `main.go`.

## 4. envtest scenarios worth having

| Scenario | Asserts |
|---|---|
| instance without licence | `Licensed=False`; **no** HelmRelease created |
| licence applied | HelmRelease created with correct values and owner reference |
| HelmRelease Ready (you set its status) | instance `Ready=True` |
| finalizer | added on first reconcile; namespace delete requested on deletion |
| licence expiry | reconcile returns `RequeueAfter` ≈ time until `NotAfter` |
| status is not overwritten | two reconciles in a row → `lastTransitionTime` unchanged |

Use `Eventually` for anything asynchronous. **Never `time.Sleep`.** A sleep is either too short
(flaky) or too long (slow), and it is usually both on a CI machine.

## 5. e2e on kind

The scaffold's `test/e2e/` builds the image, loads it into a kind cluster and deploys it. Extend it
with one scenario — the whole lifecycle:

1. apply `License` (signed in the test with a throwaway key; the operator deployed with that public key)
2. apply `ModuleInstance`
3. `Eventually` → `Ready=True`, `Licensed=True`, `metadataRevision` set
4. `curl` the module through a port-forward
5. delete the `ModuleInstance` → `Eventually` → namespace gone

For CI-friendliness, run a local registry in the test cluster rather than depending on company
Harbor; keep one manual run against Harbor as a separate, optional test.

```bash
make test          # pure + envtest
make test-e2e      # kind
```

## 6. Break it — the test suite

For each of these, change the code, confirm **a test fails**, then revert. If no test fails, write
the missing test.

| # | Sabotage |
|---|---|
| 1 | remove the `Watches(&License{})` line |
| 2 | swap `now.Before(e.NotAfter)` for `now.After(e.NotAfter)` |
| 3 | remove `SetControllerReference` from the HelmRelease |
| 4 | call `r.Update` instead of `r.Status().Update` |
| 5 | skip signature verification and just parse the payload |
| 6 | remove the `RequeueAfter` from the License controller |

This is called **mutation testing**, done by hand. Tests that keep passing when the code is wrong
are decoration.

## Done when

- [ ] table tests for `Entitled`, `licence.Verify`, `validateMetadata`
- [ ] envtest covers the six scenarios in §4
- [ ] one e2e lifecycle test passes on kind
- [ ] all six sabotages in §6 are caught
- [ ] `git commit -am "stage 11: test suite"`
