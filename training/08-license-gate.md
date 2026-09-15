# Stage 08 — The licence becomes part of the lifecycle

**Goal:** a `ModuleInstance` installs only if a valid, signed, unexpired `License` grants that module
and version. When the licence expires, the status says so — **without anyone touching anything**.
**Concepts:** a second API group, signatures, a second controller, cross-resource watches,
time-based requeue, pure decision functions.

This is where the lifecycle becomes ours: install, report, clean up, upgrade — **and only if
entitled**.

---

## 1. Signatures in two minutes

| | Encryption | Signature |
|---|---|---|
| Hides the content? | yes | **no** — anyone can read a licence |
| Proves who wrote it? | no | **yes** |
| Key that creates it | public | **private** — stays with us, never in a cluster |
| Key that checks it | private | **public** — shipped inside the operator |

A licence is not secret. The customer can read every line of it. What they must not be able to do
is **change** it — extend the date, add a module — and a signature makes any change detectable.

**Ed25519** is the algorithm: small keys, fast, no parameters to get wrong. The real platform wraps
it in JWS or PASETO. Here you use it **raw**, so you can see what those formats are doing for you.

## 2. The licence tool

A token here is `base64url(payload) + "." + base64url(signature)`. The verification logic goes in
the operator's module so both the tool and the controller share one implementation.

`internal/licence/licence.go`:

```go
package licence

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Entitlement struct {
	Module       string    `json:"module"`
	VersionRange string    `json:"versionRange"` // e.g. ">=0.1.0 <1.0.0"
	NotAfter     time.Time `json:"notAfter"`
}

type Claims struct {
	Customer     string        `json:"customer"`
	IssuedAt     time.Time     `json:"issuedAt"`
	Entitlements []Entitlement `json:"entitlements"`
}

var b64 = base64.RawURLEncoding

func Sign(priv ed25519.PrivateKey, c Claims) (string, error) {
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return b64.EncodeToString(payload) + "." + b64.EncodeToString(ed25519.Sign(priv, payload)), nil
}

// Verify checks the signature FIRST and only then parses. Parsing untrusted input before
// verifying it means acting on data an attacker controls.
func Verify(pub ed25519.PublicKey, token string) (*Claims, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 2 {
		return nil, errors.New("malformed token: expected payload.signature")
	}
	payload, err := b64.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("payload encoding: %w", err)
	}
	sig, err := b64.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("signature encoding: %w", err)
	}
	if !ed25519.Verify(pub, payload, sig) {
		return nil, errors.New("signature does not match — token altered or signed by another key")
	}
	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, fmt.Errorf("claims: %w", err)
	}
	return &c, nil
}
```

`hack/licence-tool/main.go` — a small CLI with `keygen` and `sign`:

```go
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/mr-elamin/kubebuilder-practice/internal/licence"
)

var b64 = base64.RawURLEncoding

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: licence-tool keygen | sign [flags]")
	}
	switch os.Args[1] {
	case "keygen":
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			log.Fatal(err)
		}
		// 0600: the private key is the thing that mints licences. Treat it like a root password.
		must(os.WriteFile("licence.key", []byte(b64.EncodeToString(priv)), 0o600))
		must(os.WriteFile("licence.pub", []byte(b64.EncodeToString(pub)), 0o644))
		fmt.Println("wrote licence.key (SECRET) and licence.pub")

	case "sign":
		fs := flag.NewFlagSet("sign", flag.ExitOnError)
		keyFile := fs.String("key", "licence.key", "private key file")
		customer := fs.String("customer", "acme", "customer id")
		module := fs.String("module", "hello-world", "module id")
		versions := fs.String("versions", ">=0.1.0 <1.0.0", "semver range")
		validFor := fs.Duration("valid-for", 24*time.Hour, "validity, e.g. 2m, 720h")
		must(fs.Parse(os.Args[2:]))

		raw, err := os.ReadFile(*keyFile)
		must(err)
		key, err := b64.DecodeString(strings.TrimSpace(string(raw)))
		must(err)
		if len(key) != ed25519.PrivateKeySize {
			log.Fatalf("bad key length %d", len(key))
		}

		now := time.Now().UTC()
		tok, err := licence.Sign(ed25519.PrivateKey(key), licence.Claims{
			Customer: *customer,
			IssuedAt: now,
			Entitlements: []licence.Entitlement{{
				Module: *module, VersionRange: *versions, NotAfter: now.Add(*validFor),
			}},
		})
		must(err)
		fmt.Println(tok)
	}
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
```

```bash
mkdir -p ~/.kb-training-keys && cd ~/.kb-training-keys     # OUTSIDE the repo
go run ~/tmp/kubebuilder/hack/licence-tool keygen
go run ~/tmp/kubebuilder/hack/licence-tool sign --valid-for 720h > acme.lic
cat acme.lic | cut -d. -f1 | base64 -d 2>/dev/null | jq    # readable — not secret, only signed
```

Keep keys out of the repository. Add `*.key` and `*.lic` to `.gitignore` anyway.

## 3. The `License` API — a second group

```bash
cd ~/tmp/kubebuilder
kubebuilder create api --group licensing --version v1alpha1 --kind License --resource --controller
git status          # api/licensing/… and internal/controller/licensing/ — multigroup at work
```

```go
type LicenseSpec struct {
	// Token is the signed licence exactly as issued. Opaque to the user.
	// +kubebuilder:validation:MinLength=10
	Token string `json:"token"`
}

type EntitlementStatus struct {
	Module       string      `json:"module"`
	VersionRange string      `json:"versionRange"`
	NotAfter     metav1.Time `json:"notAfter"`
	Active       bool        `json:"active"`
}

type LicenseStatus struct {
	// +optional
	Customer string `json:"customer,omitempty"`
	// +optional
	Entitlements []EntitlementStatus `json:"entitlements,omitempty"`
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

Printer columns: Customer, `Valid` condition status, Age.

## 4. The License controller

It is the **only** thing that verifies. Everything else reads its status.

```go
type LicenseReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	PublicKey ed25519.PublicKey // loaded in main.go from LICENSE_PUBLIC_KEY
}

func (r *LicenseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var lic licensingv1alpha1.License
	if err := r.Get(ctx, req.NamespacedName, &lic); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	claims, err := licence.Verify(r.PublicKey, lic.Spec.Token)
	if err != nil {
		lic.Status.Customer, lic.Status.Entitlements = "", nil
		meta.SetStatusCondition(&lic.Status.Conditions, metav1.Condition{
			Type: "Valid", Status: metav1.ConditionFalse, Reason: "VerificationFailed",
			Message: err.Error(), ObservedGeneration: lic.Generation,
		})
		// No error returned: retrying cannot fix a bad signature. Only a new token can,
		// and that is a spec change, which triggers reconcile on its own.
		return ctrl.Result{}, r.Status().Update(ctx, &lic)
	}

	now := time.Now()
	var nextExpiry time.Time
	lic.Status.Customer = claims.Customer
	lic.Status.Entitlements = nil
	for _, e := range claims.Entitlements {
		active := now.Before(e.NotAfter)
		lic.Status.Entitlements = append(lic.Status.Entitlements, licensingv1alpha1.EntitlementStatus{
			Module: e.Module, VersionRange: e.VersionRange,
			NotAfter: metav1.NewTime(e.NotAfter), Active: active,
		})
		if active && (nextExpiry.IsZero() || e.NotAfter.Before(nextExpiry)) {
			nextExpiry = e.NotAfter
		}
	}
	meta.SetStatusCondition(&lic.Status.Conditions, metav1.Condition{
		Type: "Valid", Status: metav1.ConditionTrue, Reason: "SignatureVerified",
		Message: "issued to " + claims.Customer, ObservedGeneration: lic.Generation,
	})
	if err := r.Status().Update(ctx, &lic); err != nil {
		return ctrl.Result{}, err
	}

	// THE lesson of this stage. Nothing will ever send an event when a licence expires — time
	// passing is not an API change. So the controller schedules its own wake-up for that moment.
	if !nextExpiry.IsZero() {
		return ctrl.Result{RequeueAfter: time.Until(nextExpiry) + time.Second}, nil
	}
	return ctrl.Result{}, nil
}
```

In `cmd/main.go`, read the public key from an environment variable, decode it, check its length is
`ed25519.PublicKeySize`, and pass it to the reconciler. **Fail at startup** if it is missing — an
operator that starts without a key and marks every licence invalid is much harder to diagnose.

```bash
export LICENSE_PUBLIC_KEY=$(cat ~/.kb-training-keys/licence.pub)
make manifests generate install && make run
```

## 5. Gate the ModuleInstance

First a **pure function** — no client, no context, just inputs and a decision. That is what makes it
trivial to test in stage 11:

```go
// go get github.com/Masterminds/semver/v3
func Entitled(lics []licensingv1alpha1.License, module, version string, now time.Time) (bool, string) {
	v, err := semver.NewVersion(version)
	if err != nil {
		return false, "InvalidVersion"
	}
	for _, l := range lics {
		if !meta.IsStatusConditionTrue(l.Status.Conditions, "Valid") {
			continue
		}
		for _, e := range l.Status.Entitlements {
			if e.Module != module || !now.Before(e.NotAfter.Time) {
				continue
			}
			c, err := semver.NewConstraint(e.VersionRange)
			if err == nil && c.Check(v) {
				return true, "Entitled"
			}
		}
	}
	return false, "NoEntitlement"
}
```

Note it checks `NotAfter` against `now` itself, rather than trusting `Active` in the status — status
can be up to one reconcile stale.

In the `ModuleInstance` reconciler, after the finalizer logic and **before** creating the Flux
objects:

```go
	var lics licensingv1alpha1.LicenseList
	if err := r.List(ctx, &lics, client.InNamespace(mi.Namespace)); err != nil {
		return ctrl.Result{}, err
	}
	ok, reason := Entitled(lics.Items, mi.Spec.Module, mi.Spec.Version, time.Now())

	licensed := metav1.Condition{Type: "Licensed", ObservedGeneration: mi.Generation, Reason: reason}
	if ok {
		licensed.Status = metav1.ConditionTrue
	} else {
		licensed.Status = metav1.ConditionFalse
		licensed.Message = fmt.Sprintf("no valid licence for %s %s", mi.Spec.Module, mi.Spec.Version)
	}
	meta.SetStatusCondition(&mi.Status.Conditions, licensed)

	if !ok {
		// Not entitled. Decide carefully what that means for something ALREADY running:
		// here we leave an existing HelmRelease untouched (no new install, no upgrade) rather
		// than uninstalling. A licence lapse must never become a customer's outage at midnight.
		return ctrl.Result{}, r.Status().Update(ctx, &mi)
	}
```

**Wake instances up when licences change.** Without this, applying a licence does nothing until
something else happens to touch the `ModuleInstance`:

```go
func (r *ModuleInstanceReconciler) instancesForLicense(ctx context.Context, obj client.Object) []reconcile.Request {
	var list platformv1alpha1.ModuleInstanceList
	if err := r.List(ctx, &list, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}
	reqs := make([]reconcile.Request, 0, len(list.Items))
	for _, mi := range list.Items {
		reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&mi)})
	}
	return reqs
}

// in SetupWithManager:
		Watches(&licensingv1alpha1.License{}, handler.EnqueueRequestsFromMapFunc(r.instancesForLicense)).
```

Add `Licensed` to the printer columns.

## 6. Run the lifecycle

```bash
kubectl apply -f config/samples/platform_v1alpha1_moduleinstance.yaml
kubectl get moduleinstance            # Licensed=False, NoEntitlement — nothing installed

cat <<EOF | kubectl apply -f -
apiVersion: licensing.bssconnects.io/v1alpha1
kind: License
metadata:
  name: acme
spec:
  token: $(cat ~/.kb-training-keys/acme.lic)
EOF

kubectl get license,moduleinstance -w  # Valid=True → Licensed=True → Flux installs → Ready
```

## 7. Break it

| # | Do this | Question |
|---|---|---|
| 1 | Change one character in the middle of the token | What does `License` status say? What happens to the instance? |
| 2 | Sign a licence with a **newly generated** key | Same signature algorithm, same claims — why is it rejected? |
| 3 | Sign with `--valid-for 3m`. Apply. Do nothing, and watch for 4 minutes | What flips, when, and what caused the reconcile? Which line? |
| 4 | Sign with `--versions ">=0.1.0 <0.2.0"`, then set `version: 0.2.0` | Is it installed? What happens to the 0.1.0 release already running? |
| 5 | `kubectl delete license acme` while hello-world is running | Is it uninstalled? Should it be? Argue both sides in `notes/08.md`. |
| 6 | Decode the payload, change the date, re-encode, keep the old signature | Exactly which check catches it? |
| 7 | Make your laptop clock wrong (or fake `now` in a test) | What does clock skew do to licensing at a customer site? |

**Question 5 is a product decision, not an engineering one.** Write down the two options and their
consequences — exactly the kind of thing the real project turns into an ADR.

## Done when

- [ ] no licence → nothing installed, with an honest `Licensed=False`
- [ ] applying a licence installs without touching the `ModuleInstance`
- [ ] expiry flips status on its own at the right moment
- [ ] a tampered token is rejected with a clear message
- [ ] `notes/08.md` answers question 5 with both options
- [ ] `git commit -am "stage 08: licence-gated lifecycle"`
