# Stage 00 — A safe place to break things

**Goal:** a local kind cluster with a local OCI registry, and the habit of checking your context.
**Why first:** your shell is currently pointed at the `bss` cluster. `make install` and
`make deploy` apply to *whatever* context is current. A practice CRD in a real cluster is the kind
of mistake that ends up in a postmortem.

---

## 1. Tools

```bash
go version              # 1.23+
kubebuilder version
kind version
kubectl version --client
helm version            # 3.13+ — needed for `helm push --plain-http`
docker version
```

You will install these later, at the stage that needs them — not now:

| Tool | Stage | Install |
|---|---|---|
| `oras` | 05 | https://oras.land/docs/installation |
| `flux` | 07 | https://fluxcd.io/flux/installation/ |
| `cosign` | 06 (optional) | https://docs.sigstore.dev/cosign/system_config/installation/ |

## 2. A local registry, then the cluster

The registry is for stage 05. Creating it now means the cluster is wired to it from the start.

```bash
# A plain OCI registry on the host. 127.0.0.1 only — nothing else on your network can reach it.
docker run -d --restart=always -p 127.0.0.1:5001:5000 --name kind-registry registry:2

# Tell containerd inside kind to read per-registry config from a directory.
cat <<EOF | kind create cluster --name kb-practice --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry]
    config_path = "/etc/containerd/certs.d"
nodes:
- role: control-plane
- role: worker     # a second node catches scheduling assumptions a single node hides
EOF
```

Now the part that trips everyone: **inside a kind node, `localhost` is the node itself**, not your
laptop. So an image called `localhost:5001/hello-world` would be looked for *inside the node* and
not found. This maps that name to the registry container instead:

```bash
for node in $(kind get nodes --name kb-practice); do
  docker exec "$node" mkdir -p /etc/containerd/certs.d/localhost:5001
  echo '[host."http://kind-registry:5000"]' | \
    docker exec -i "$node" cp /dev/stdin /etc/containerd/certs.d/localhost:5001/hosts.toml
done

# Put the registry on kind's docker network so the name `kind-registry` resolves from the nodes.
docker network connect kind kind-registry 2>/dev/null || true
```

If this does not work with your kind version, use the current script from
https://kind.sigs.k8s.io/docs/user/local-registry/ — containerd's config format has changed between
versions, and that page tracks it.

## 3. The context check — make it a reflex

```bash
kubectl config use-context kind-kb-practice
kubectl config current-context        # kind-kb-practice
kubectl get nodes                     # two nodes, both Ready
```

**Exercise:** protect yourself permanently. Add this to the operator's `Makefile`, and make
`install`, `uninstall`, `deploy` and `undeploy` depend on it:

```makefile
.PHONY: check-context
check-context: ## Refuse to touch any cluster other than the practice one
	@ctx=$$(kubectl config current-context); \
	if [ "$$ctx" != "kind-kb-practice" ]; then \
	  echo "refusing: current context is '$$ctx', expected kind-kb-practice"; exit 1; \
	fi
```

Then change, for example, `install: manifests kustomize` to `install: check-context manifests kustomize`.

Test it: switch back to your `bss` context and run `make install`. It must refuse. Switch back.

## Done when

- [ ] `kubectl get nodes` shows two Ready nodes in `kind-kb-practice`
- [ ] `docker ps` shows `kind-registry`
- [ ] `make install` refuses to run against any other context
- [ ] `git commit -am "stage 00: safe environment"`
