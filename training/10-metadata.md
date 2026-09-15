# Stage 10 — Learn what the module offers: fetching `__metadata__`

**Goal:** once hello-world is Ready, the operator fetches its `__metadata__`, validates it, and
records what it found — without doing network I/O on every reconcile.
**Concepts:** controllers calling services, timeouts, not blocking reconciles, change detection,
why `metadataRevision` must be a content hash.

This is the first step toward the real platform's **Module Registry**, where the operator fetches
each module's contract once and publishes it for the UI, the CLI and the authorization service.

---

## 1. Where it goes

Keep it small:

| What | Where |
|---|---|
| `metadataRevision`, number of permissions, fetched-for-version | `ModuleInstance.status` |
| the full document | a ConfigMap `module-metadata-<instance>` in the instance's namespace, owned by the instance |

The real platform uses a dedicated registry object instead of a ConfigMap. The mechanics are the
same.

## 2. Status fields

```go
	// MetadataRevision is the revision the module declared in its __metadata__.
	// +optional
	MetadataRevision string `json:"metadataRevision,omitempty"`

	// MetadataFetchedFor is the module version the metadata was fetched for. Used to decide
	// whether to fetch again — the network is NOT touched on every reconcile.
	// +optional
	MetadataFetchedFor string `json:"metadataFetchedFor,omitempty"`
```

## 3. Fetch — carefully

```go
type moduleMetadata struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Module     struct {
		ID               string `json:"id"`
		Version          string `json:"version"`
		MetadataRevision string `json:"metadataRevision"`
	} `json:"module"`
	Permissions []struct {
		ID string `json:"id"`
	} `json:"permissions"`
}

// A package-level client WITH a timeout. http.DefaultClient has none: one module that accepts the
// connection and never answers would hang this reconcile forever and stall the whole worker queue.
var metadataHTTP = &http.Client{Timeout: 5 * time.Second}

func fetchMetadata(ctx context.Context, url string) ([]byte, *moduleMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := metadataHTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	// Never read an unbounded body from a service you do not control.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, nil, err
	}
	var m moduleMetadata
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, nil, fmt.Errorf("not valid JSON: %w", err)
	}
	return raw, &m, nil
}

// validateMetadata is a PURE function — test it without a cluster in stage 11.
func validateMetadata(m *moduleMetadata, expectedModule string) error {
	if m.Kind != "ModuleMetadata" {
		return fmt.Errorf("kind is %q, want ModuleMetadata", m.Kind)
	}
	if m.Module.ID != expectedModule {
		return fmt.Errorf("module.id is %q but this instance installs %q", m.Module.ID, expectedModule)
	}
	if m.Module.MetadataRevision == "" {
		return errors.New("module.metadataRevision is required")
	}
	for _, p := range m.Permissions {
		// A module must not declare permissions in another module's namespace.
		if !strings.HasPrefix(p.ID, m.Module.ID+":") {
			return fmt.Errorf("permission %q must start with %q", p.ID, m.Module.ID+":")
		}
	}
	return nil
}
```

## 4. Wire it into `Reconcile`

After mirroring Flux's status, and only when it makes sense to fetch:

```go
	ready := meta.IsStatusConditionTrue(mi.Status.Conditions, "Ready")
	needsFetch := ready && mi.Status.MetadataFetchedFor != mi.Spec.Version

	if needsFetch {
		// Service name = release name = chart name (stage 05). The operator runs in-cluster since
		// stage 09, so cluster DNS resolves. With `make run` on a laptop this URL would not.
		url := fmt.Sprintf("http://%s.%s.svc.cluster.local:8080/__metadata__",
			mi.Spec.Module, moduleNamespace(&mi))

		raw, m, err := fetchMetadata(ctx, url)
		if err == nil {
			err = validateMetadata(m, mi.Spec.Module)
		}

		cond := metav1.Condition{Type: "MetadataAvailable", ObservedGeneration: mi.Generation}
		if err != nil {
			cond.Status, cond.Reason, cond.Message = metav1.ConditionFalse, "FetchOrValidationFailed", err.Error()
			meta.SetStatusCondition(&mi.Status.Conditions, cond)
			if uerr := r.Status().Update(ctx, &mi); uerr != nil {
				return ctrl.Result{}, uerr
			}
			// Try again later, WITHOUT returning an error: a broken module is not a controller
			// failure, and error backoff would hide it among real errors in the metrics.
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}

		// store raw in a ConfigMap owned by mi — your turn: CreateOrUpdate + SetControllerReference

		mi.Status.MetadataRevision = m.Module.MetadataRevision
		mi.Status.MetadataFetchedFor = mi.Spec.Version
		cond.Status, cond.Reason = metav1.ConditionTrue, "Fetched"
		cond.Message = fmt.Sprintf("revision %s, %d permissions", m.Module.MetadataRevision, len(m.Permissions))
		meta.SetStatusCondition(&mi.Status.Conditions, cond)
	}
```

Add a printer column for `.status.metadataRevision`. Add the RBAC marker for `configmaps`.

## 5. Break it

| # | Do this | Question |
|---|---|---|
| 1 | Make hello-world return `"id": "goodbye"` in its metadata, release as `0.1.1`, upgrade | Condition? Is the module still Ready? **Should** a bad contract uninstall a working module? |
| 2 | Make `/__metadata__` sleep 30 seconds | What does your reconcile do? What did the 5-second timeout save you from? |
| 3 | Change a permission in the metadata **without** changing the version, rebuild with the same tag | Does the operator notice? Why not? |
| 4 | Scale hello-world to 0 while the operator tries to fetch | What happens, and does it recover by itself? |
| 5 | `make run` locally instead of in-cluster | Why does the fetch fail? |

**Number 3 answers the question in stage 05's code comment.** `metadataRevision = version` means the
contract can change without the revision changing, so nothing downstream knows to refresh. That is
why the real module contract requires `metadataRevision` to be **generated from a hash of the
document's content** in the build, never maintained by hand. Write the one-line fix for hello-world's
build in `notes/10.md`.

## Done when

- [ ] Ready instances show a metadata revision in `kubectl get`
- [ ] invalid metadata produces a clear condition without uninstalling the module
- [ ] the metadata is fetched once per version, not every reconcile (prove it from the logs)
- [ ] `notes/10.md` explains break-it #3 and the content-hash fix
- [ ] `git commit -am "stage 10: module metadata"`
