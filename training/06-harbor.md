# Stage 06 — Harbor: projects, robot accounts, and pulling with least privilege

**Goal:** hello-world's image, chart and metadata live in a private Harbor project; your kind cluster
pulls them using a **pull-only robot account**; you can explain why that robot is a licensing
control, not just a credential.
**Concepts:** Harbor projects, robot accounts, the `$` trap, pull secrets, Harbor's API, tag
immutability.

---

## 1. Why this matters for our platform

In the real design, **each customer gets a Harbor robot account derived from their licence**, with
pull permission only on the projects they have paid for. No licence → no robot → no image. The
registry becomes an enforcement point, not just storage.

This stage builds the intuition for that with one project and two robots.

## 2. Concepts

| Concept | What it is |
|---|---|
| **Project** | a namespace in Harbor: `harbor.example.com/<project>/<repository>`. Public or private. Quotas, scanning, retention and immutability rules are set per project. |
| **Repository** | inside a project: `kb-training/images/hello-world`, `kb-training/charts/hello-world` |
| **Artifact** | anything pushed: image, chart, signature, SBOM, your metadata document |
| **Member** | a human (usually via OIDC) with a role on a project |
| **Robot account** | a **non-human** credential with fine-grained permissions and an expiry. Machines use robots; humans use their own login. |
| **Project-level robot** | scoped to one project. Named `robot$<project>+<name>` |
| **System-level robot** | can span several projects. Named `robot$<name>`. Created by admins. |
| **Tag immutability rule** | stops a tag being overwritten — the fix for break-it #1 in stage 05 |
| **Replication** | copies artifacts to another registry — how an air-gapped customer site gets its mirror |

Harbor's web UI has an API explorer at `https://<your-harbor>/devcenter-api-2.0`. Keep it open;
everything the UI does, the API does.

## 3. Setup

Use a **dedicated training project** on your company Harbor, not a production project. If you do not
have admin rights, ask for a private project called `kb-training` and permission to create robots in
it.

```bash
export HARBOR=harbor.your-company.example      # your Harbor host, no https://
```

If your Harbor uses an **internal CA**, the kind nodes and later Flux must trust it. Note it now; it
is the most common reason the pull in step 6 fails. (Kind: add the CA under
`/etc/containerd/certs.d/$HARBOR/` on each node, like the local registry in stage 00. Flux: a
`certSecretRef` on the source.)

## 4. Two robots, two jobs

In the Harbor UI → project `kb-training` → **Robot Accounts** → **New Robot Account**:

| Robot | Permissions | Used by | Expiry |
|---|---|---|---|
| `ci-pusher` | repository push + pull | you, standing in for CI | 30 days |
| `cluster-puller` | repository **pull only** | the kind cluster | 30 days |

**Copy each secret immediately.** Harbor shows it once. If you lose it, you refresh it — which
invalidates the old one everywhere it is used.

Why two, not one with push and pull? Because the credential sitting in a cluster is the one most
likely to leak. If `cluster-puller` leaks, an attacker can read your images. If a combined robot
leaks, they can **replace** your images with their own. Least privilege is not a checklist item
here; it is the difference between a data leak and a supply-chain compromise.

## 5. The `$` trap — read this before typing any robot name

```bash
# WRONG. Inside double quotes the shell expands $kb as a variable (empty),
# so Harbor receives the username "robot-training+ci-pusher" and rejects it.
docker login $HARBOR -u "robot$kb-training+ci-pusher"

# RIGHT. Single quotes: no expansion.
docker login $HARBOR -u 'robot$kb-training+ci-pusher'
```

This will cost you twenty minutes exactly once. It is cheaper to read it now. It also bites in
Kubernetes Secret YAML, CI variables, and Helm `--set` values.

## 6. Push with the pusher, pull with the puller

```bash
read -rsp 'ci-pusher secret: ' PUSHER_SECRET; echo

echo "$PUSHER_SECRET" | docker login "$HARBOR" -u 'robot$kb-training+ci-pusher' --password-stdin
echo "$PUSHER_SECRET" | helm registry login "$HARBOR" -u 'robot$kb-training+ci-pusher' --password-stdin
echo "$PUSHER_SECRET" | oras login "$HARBOR" -u 'robot$kb-training+ci-pusher' --password-stdin

cd ~/tmp/kubebuilder/hello-world
docker tag localhost:5001/images/hello-world:0.1.0 "$HARBOR/kb-training/images/hello-world:0.1.0"
docker push "$HARBOR/kb-training/images/hello-world:0.1.0"
helm push hello-world-0.1.0.tgz "oci://$HARBOR/kb-training/charts"
oras push "$HARBOR/kb-training/modules/hello-world-metadata:0.1.0" \
  --artifact-type application/vnd.bssconnects.module.metadata.v1+json \
  __metadata__.json:application/json
```

`--password-stdin` and `read -s` keep the secret out of your shell history and out of `ps` output.

Look at the project in the Harbor UI: you should see all three artifacts, each with a different
type.

Now the cluster side:

```bash
read -rsp 'cluster-puller secret: ' PULLER_SECRET; echo

kubectl create namespace harbor-test
kubectl -n harbor-test create secret docker-registry harbor-puller \
  --docker-server="$HARBOR" \
  --docker-username='robot$kb-training+cluster-puller' \
  --docker-password="$PULLER_SECRET"

kubectl -n harbor-test run hw --image="$HARBOR/kb-training/images/hello-world:0.1.0" \
  --overrides='{"spec":{"imagePullSecrets":[{"name":"harbor-puller"}]}}'
kubectl -n harbor-test get pod hw -w
```

## 7. Create a robot through the API

The platform's licence tooling will create customer robots automatically, not by clicking. Do it
once by hand so you know what that code will do:

```bash
curl -sS -u 'your-harbor-user' -H 'Content-Type: application/json' \
  -X POST "https://$HARBOR/api/v2.0/robots" -d '{
    "name": "customer-acme",
    "description": "pull access for customer acme, derived from licence acme-2026",
    "duration": 30,
    "level": "project",
    "permissions": [{
      "kind": "project",
      "namespace": "kb-training",
      "access": [ { "resource": "repository", "action": "pull" } ]
    }]
  }' | jq
```

The response contains the full robot name and its secret. `duration` is in days; `-1` means never
expires — do not use that for anything tied to a licence that has an end date. Check the exact body
against your Harbor version in the API explorer.

## 8. Break it

| # | Do this | Question |
|---|---|---|
| 1 | Try `docker push` using `cluster-puller` | What error? Is it clear enough that a customer's support engineer would understand it? |
| 2 | Disable `cluster-puller` in the UI, then `kubectl -n harbor-test delete pod hw` and run it again | Does it fail? Now: what if the pod had **not** been deleted? |
| 3 | Same as 2, but check `imagePullPolicy` of the original pod and which node it ran on | Why can a cluster keep running an image after its credential is revoked? What does that mean for "revoke the licence → stop the software"? |
| 4 | Add a **tag immutability rule** for `0.*` on the project, then push `0.1.0` again | What happens? Compare with stage 05 break-it #1. |
| 5 | Use the double-quoted robot name on purpose | Read the exact error so you recognise it next time. |

**Number 3 is a real design lesson for our platform.** Revoking a registry credential stops *future*
pulls. It does not stop images already on a node. That is exactly why the design enforces the licence
**at runtime inside the module** as well — the registry alone cannot switch software off.

## Done when

- [ ] image, chart and metadata in Harbor, pushed with `ci-pusher`
- [ ] a pod in kind pulls with `cluster-puller`
- [ ] a robot created through the API
- [ ] `notes/06.md` explains, in your own words, why two robots and why revocation does not stop running pods
- [ ] clean up: `kubectl delete ns harbor-test`
