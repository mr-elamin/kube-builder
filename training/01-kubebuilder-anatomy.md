# Stage 01 — What `kubebuilder init` gave you

**Goal:** understand every file in your tree before adding to it.
**No code this stage.** Read, answer the questions in `notes/01.md`, run a few commands.

---

## The mental model first

```
┌──────────────────────── the manager (one process, cmd/main.go) ─────────────────────────┐
│                                                                                           │
│   shared cache  ◄── watches ──  API server        every controller reads from this cache,  │
│   (informers)                                     not from the API server directly        │
│        │                                                                                  │
│        ├──► controller A ──► Reconcile(ModuleInstance)     ← you write these              │
│        └──► controller B ──► Reconcile(License)                                           │
│                                                                                           │
│   also: metrics endpoint · health probes · leader election · webhook server (later)       │
└───────────────────────────────────────────────────────────────────────────────────────────┘
```

- **Manager** — the process. Owns the cache, the clients, and runs all controllers.
- **Controller** — watches some kinds and queues work.
- **Reconciler** — your function. Given a name, make the world match that object's spec.

You have a manager with **zero controllers** right now. Stage 02 adds the first.

## File by file

| Path | What it is | Will you edit it? |
|---|---|---|
| `PROJECT` | kubebuilder's record of your domain, layout, and every API you create | never by hand |
| `Makefile` | the entry points — `make help` lists them | occasionally |
| `cmd/main.go` | builds the manager, registers schemes and controllers | yes, lightly |
| `config/default/` | the kustomize overlay that ties everything together for `make deploy` | yes, to enable webhooks later |
| `config/manager/` | the Deployment that runs your operator in-cluster | rarely |
| `config/rbac/` | ServiceAccount, roles, bindings. `role.yaml` is **generated** from `+kubebuilder:rbac` markers | never edit `role.yaml` |
| `config/prometheus/` | a `ServiceMonitor` for the operator's metrics | no |
| `config/network-policy/` | restricts who may scrape metrics | no |
| `hack/boilerplate.go.txt` | the licence header stamped on generated files | no |
| `test/e2e/` | end-to-end tests that run against a kind cluster | stage 11 |
| `Dockerfile` | multi-stage build of the manager image | no |
| `AGENTS.md` | conventions for AI coding assistants working in this repo — also a decent summary for humans | read it |

Coming at stage 02: `api/` and `internal/controller/`.

## Commands to run and understand

```bash
make help            # read every target's description
cat PROJECT          # compare with the flags you passed to init
make build           # compiles bin/manager
git diff             # nothing should have changed — build output is gitignored
```

Now run the manager with no controllers:

```bash
kubectl config current-context    # kind-kb-practice — ALWAYS check first
make run
```

It starts, logs a few lines, and does nothing. `Ctrl-C` to stop.

**`make run` runs the manager on your laptop, using your current kubeconfig.** That is the fastest
development loop you will have: no image build, no deploy, instant restarts. You will use it for
stages 02–08 and only switch to in-cluster deployment when webhooks force you to in stage 09.

## Questions — answer in `notes/01.md`

Open `cmd/main.go` and the Makefile. Find the answers yourself; write them in your own words.

1. Where is the **scheme** built, and what is a scheme for? What would fail if a type were missing
   from it?
2. What does **leader election** do? Why is it off by default when you `make run`, and why would
   you need it on with two replicas?
3. What address does the **metrics** endpoint bind to by default, and why is it secured?
4. There is a comment about disabling **HTTP/2**. What vulnerability is it about?
5. Which tool does `make manifests` call, and which directories does it write to?
6. In `config/default/kustomization.yaml`, what do `namespace` and `namePrefix` do to every
   resource? Where did the value come from?
7. What is the final base image in the `Dockerfile`, and why that image?
8. What is the difference between `make run` and `make deploy` in terms of **where the code runs**
   and **which permissions it has**? (Hint: one uses your kubeconfig.)

Question 8 matters more than it looks. With `make run` your operator has **your** permissions —
probably cluster-admin — so a missing RBAC marker goes unnoticed until the first `make deploy`,
when the operator runs as its own ServiceAccount and suddenly gets `forbidden`. Remember this at
stage 09.

## Done when

- [ ] all eight questions answered in `notes/01.md`
- [ ] `make run` starts cleanly against `kind-kb-practice`
- [ ] you can explain manager / controller / reconciler to a colleague without notes
