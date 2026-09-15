# Stage 05 — hello-world becomes a real module: an image and a Helm chart in an OCI registry

**Goal:** your own hello-world image and chart, both stored in the local OCI registry, installable
with `helm install oci://...`.
**Concepts:** OCI registries, artifacts, manifests, tags vs digests, Helm charts as OCI artifacts,
ORAS.
**The operator does not change this stage.** You are building what stage 07 will install.

---

## 1. OCI, in five minutes

An OCI registry is not just for container images. It stores **artifacts** — any content, described
by a manifest.

```
registry         localhost:5001
 └─ repository   charts/hello-world
     ├─ tag      0.1.0  ──► points to a manifest          (tags are MOVABLE labels)
     └─ manifest sha256:4f1c…                              (digests are IMMUTABLE)
          ├─ config   mediaType tells you WHAT this is
          │           application/vnd.oci.image.config.v1+json          → a container image
          │           application/vnd.cncf.helm.config.v1+json          → a Helm chart
          └─ layers   the actual content blobs
```

The **media type** is what lets one registry hold images, charts, signatures, SBOMs — and in our
real platform, each module's `__metadata__` document — side by side.

**Tags move, digests do not.** `hello-world:0.1.0` can be overwritten tomorrow with different
content. `hello-world@sha256:4f1c…` cannot. Production should pin digests; you will see why in the
break-it section.

## 2. Build your own hello-world

Create a **separate** Go module, outside the operator — a module and the operator that installs it
are different codebases:

```bash
mkdir -p ~/tmp/kubebuilder/hello-world && cd ~/tmp/kubebuilder/hello-world
go mod init example.com/hello-world
```

`main.go`:

```go
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

// Set at build time: -ldflags "-X main.version=0.2.0". One binary, version baked in, no config drift.
var version = "0.1.0"

func main() {
	msg := os.Getenv("MESSAGE")
	if msg == "" {
		msg = "hello, world"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(msg + " (v" + version + ")\n"))
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// The module contract, in miniature. Stage 10 makes the operator read this.
	mux.HandleFunc("GET /__metadata__", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"apiVersion": "platform.bssconnects.io/v1alpha1",
			"kind":       "ModuleMetadata",
			"module": map[string]any{
				"id":               "hello-world",
				"version":          version,
				"metadataRevision": version, // stage 10 asks you why this is not good enough
			},
			"permissions": []map[string]string{
				{"id": "hello-world:greeting:read", "displayName": "Read greetings"},
			},
		})
	})

	log.Printf("hello-world %s listening on :8080", version)
	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

`Dockerfile`:

```dockerfile
FROM golang:1.23 AS build
WORKDIR /src
COPY . .
ARG VERSION=0.1.0
# Static binary: no libc, so it runs on a distroless base with nothing else in the image.
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION}" -o /hello-world .

FROM gcr.io/distroless/static:nonroot
COPY --from=build /hello-world /hello-world
USER 65532:65532
ENTRYPOINT ["/hello-world"]
```

```bash
docker build --build-arg VERSION=0.1.0 -t localhost:5001/images/hello-world:0.1.0 .
docker push localhost:5001/images/hello-world:0.1.0
```

## 3. Make it a Helm chart

```bash
helm create chart && mv chart hello-world-chart
```

`helm create` generates far more than you need. Trim it: delete `templates/hpa.yaml`,
`templates/ingress.yaml`, `templates/tests/`, `templates/httproute.yaml` if present. Then:

`Chart.yaml` — set `name: hello-world`, `version: 0.1.0`, `appVersion: "0.1.0"`.

`values.yaml` — replace with just what matters:

```yaml
replicaCount: 1
message: "hello, world"
image:
  repository: localhost:5001/images/hello-world
  tag: "0.1.0"
  pullPolicy: IfNotPresent
service:
  port: 8080
```

In `templates/deployment.yaml`, set the container port to `8080`, point both probes at `/healthz`,
and add the environment variable:

```yaml
          env:
            - name: MESSAGE
              value: {{ .Values.message | quote }}
```

Add `values.schema.json` — **this is what the UI will one day turn into an install form**, and what
our operator's webhook validates against:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "replicaCount": { "type": "integer", "minimum": 0, "maximum": 5 },
    "message":      { "type": "string", "minLength": 1, "maxLength": 200 }
  }
}
```

```bash
helm lint hello-world-chart
helm template hello-world hello-world-chart | less        # read what it renders
helm template hello-world hello-world-chart --set replicaCount=99   # schema must reject this
```

## 4. Push the chart as an OCI artifact

```bash
helm package hello-world-chart                    # → hello-world-0.1.0.tgz
helm push hello-world-0.1.0.tgz oci://localhost:5001/charts --plain-http
```

Now look inside what you pushed — this is the point of the stage:

```bash
oras repo tags localhost:5001/charts/hello-world --plain-http
oras manifest fetch localhost:5001/charts/hello-world:0.1.0 --plain-http | jq
```

Find the config media type and the layer media type. Write both in `notes/05.md` — stage 07 needs
the layer one.

Install it by hand to prove it works:

```bash
kubectl create ns hw-manual
helm install hello-world oci://localhost:5001/charts/hello-world --version 0.1.0 \
  -n hw-manual --plain-http
kubectl -n hw-manual port-forward svc/hello-world 8080:8080 &
curl localhost:8080 ; curl localhost:8080/__metadata__ | jq
helm uninstall hello-world -n hw-manual && kubectl delete ns hw-manual
```

Note the Service is named `hello-world` because the **release name equals the chart name**. Stage 10
depends on that — write down why.

## 5. Push the metadata as its own artifact

Our real platform stores each module's metadata in Harbor next to its chart, so the catalogue can
show a module **before** it is installed. Try that with ORAS:

```bash
curl -s localhost:8080/__metadata__ > __metadata__.json    # while port-forwarded, or write it by hand
oras push localhost:5001/modules/hello-world-metadata:0.1.0 --plain-http \
  --artifact-type application/vnd.bssconnects.module.metadata.v1+json \
  __metadata__.json:application/json

oras manifest fetch localhost:5001/modules/hello-world-metadata:0.1.0 --plain-http | jq
oras pull localhost:5001/modules/hello-world-metadata:0.1.0 --plain-http -o /tmp/meta
```

A custom `artifactType` you just invented, stored in a standard registry. That is how the platform
distributes more than images.

## 6. Break it

| # | Do this | Question |
|---|---|---|
| 1 | Change the message default, rebuild, push **the same tag** `0.1.0` | What does `oras manifest fetch` show for the digest now? What would a cluster that already has `0.1.0` cached run? |
| 2 | Install using the digest: `--version` can't do that — find how `helm install` accepts `oci://…@sha256:…` | Why would a platform operator prefer this? |
| 3 | `helm push` the same chart version twice | Does the registry refuse? Should it? (Harbor can — stage 06.) |
| 4 | Build 0.2.0 and push it alongside 0.1.0 | `oras repo tags` — how would an operator pick "the newest 0.x"? |

## Done when

- [ ] image, chart and metadata artifact all in `localhost:5001`
- [ ] `helm install oci://…` works and `curl` returns your message with the version
- [ ] both media types recorded in `notes/05.md`
- [ ] you can explain tag vs digest and why mutable tags are dangerous
- [ ] commit the `hello-world` repo separately: `cd ~/tmp/kubebuilder/hello-world && git init && git add -A && git commit -m "stage 05"`
