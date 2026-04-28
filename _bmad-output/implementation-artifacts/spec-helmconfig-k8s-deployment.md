---
title: 'Add Helm Chart Kubernetes Deployment'
type: 'feature'
created: '2026-04-28'
status: 'done'
baseline_commit: 'fa1b0dd76f9c485ec6dff8f6e13ee11d37f60593'
context:
  - '{project-root}/_bmad-output/project-context.md'
---

<frozen-after-approval reason="human-owned intent - do not modify unless human renegotiates">

## Intent

**Problem:** The deployment package supports Docker Compose and binary/systemd installs, but does not provide a Kubernetes-native installation path. Users who run Sub2API in Kubernetes need a Helm chart that can install the application with its PostgreSQL and Redis dependencies, persistence, secrets, service exposure, and health probes.

**Approach:** Add a first-party Helm chart under `deploy/helm/sub2api` that mirrors the current Docker Compose defaults while exposing safe values for Kubernetes operators. Document install, upgrade, uninstall, secret, persistence, ingress, and external database/cache usage in `deploy/README.md`.

## Boundaries & Constraints

**Always:** Preserve existing Docker Compose and binary deployment behavior; keep the chart self-contained under `deploy/helm/sub2api`; default to `weishaw/sub2api:latest`, PostgreSQL `18-alpine`, Redis `8-alpine`, `AUTO_SETUP=true`, `SERVER_HOST=0.0.0.0`, `SERVER_PORT=8080`, `RUN_MODE=standard`, and `TZ=Asia/Shanghai`; store application data, PostgreSQL data, and Redis data on PVCs when persistence is enabled; support generated or user-provided secrets for `POSTGRES_PASSWORD`, `JWT_SECRET`, `TOTP_ENCRYPTION_KEY`, `ADMIN_PASSWORD`, and `REDIS_PASSWORD`; support external PostgreSQL and external Redis by disabling bundled services and setting connection values; include probes compatible with `/health`.

**Ask First:** Adding a chart publishing workflow, adopting third-party chart dependencies, changing container image names/tags, changing application defaults, or requiring an ingress controller-specific API.

**Never:** Do not remove or rewrite existing Compose files; do not commit real credentials; do not require cluster-admin permissions; do not add app code changes unless the chart cannot function without them.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Default install | `helm install sub2api deploy/helm/sub2api` with no custom values | Renders app, PostgreSQL, Redis, PVCs, service, config secret, generated secret templates, and health probes | Helm render must fail only on invalid templates, not on absent optional values |
| User-provided secrets | Values set existing secret names or explicit password values | Templates reference existing secrets or create chart-managed secrets without exposing credentials in docs | Missing existing secrets are left for Kubernetes admission/runtime to report |
| External data services | `postgresql.enabled=false` and/or `redis.enabled=false` with host/port/user/password values | App deployment points to external services and does not render disabled bundled workloads | Values comments document required fields for a working external setup |
| Persistence disabled | `persistence.enabled=false` or dependency persistence disabled | Workloads use `emptyDir` for the relevant storage | Documentation warns that data will not survive pod rescheduling |
| Ingress enabled | `ingress.enabled=true` with host and annotations | Renders a networking.k8s.io/v1 Ingress for the app service | No controller-specific assumptions are hardcoded |

</frozen-after-approval>

## Code Map

- `deploy/docker-compose.yml` -- authoritative container defaults for Sub2API, PostgreSQL, Redis, health checks, persistence, and environment variables.
- `deploy/docker-compose.local.yml` -- local-directory persistence variant and operator-facing defaults to mirror in values comments.
- `deploy/.env.example` -- broad environment variable surface for Helm `extraEnv`.
- `deploy/README.md` -- deployment entry-point documentation that needs a Helm/Kubernetes section and file table updates.
- `Dockerfile` -- confirms the runtime image entrypoint, exposed port, non-root runtime user, and `/health` check.

## Tasks & Acceptance

**Execution:**
- [x] `deploy/helm/sub2api/Chart.yaml` -- create chart metadata for the first-party Helm deployment artifact.
- [x] `deploy/helm/sub2api/values.yaml` -- define documented defaults for image, service, ingress, persistence, resources, secrets, app env, bundled PostgreSQL, bundled Redis, external services, probes, pod settings, and `extraEnv`.
- [x] `deploy/helm/sub2api/templates/_helpers.tpl` -- add consistent resource naming and label helpers.
- [x] `deploy/helm/sub2api/templates/secret.yaml` -- render chart-managed secrets when existing secret names are not supplied.
- [x] `deploy/helm/sub2api/templates/deployment.yaml` -- render the Sub2API Deployment with PVC/emptyDir storage, env wiring, probes, security context, resources, scheduling options, and optional extra env.
- [x] `deploy/helm/sub2api/templates/service.yaml` -- expose the app through a Kubernetes Service.
- [x] `deploy/helm/sub2api/templates/ingress.yaml` -- render optional networking.k8s.io/v1 Ingress.
- [x] `deploy/helm/sub2api/templates/pvc.yaml` -- render the app data PVC when persistence is enabled.
- [x] `deploy/helm/sub2api/templates/postgresql.yaml` -- render optional bundled PostgreSQL Service, PVC/emptyDir, and Deployment with health probe and `PGDATA=/var/lib/postgresql/data`.
- [x] `deploy/helm/sub2api/templates/redis.yaml` -- render optional bundled Redis Service, PVC/emptyDir, Deployment, password-aware command, and health probe.
- [x] `deploy/helm/sub2api/templates/NOTES.txt` -- show release URL hints and admin password log guidance.
- [x] `deploy/README.md` -- add Helm/Kubernetes to deployment methods, file list, and usage instructions.

**Acceptance Criteria:**
- Given default values, when `helm template sub2api deploy/helm/sub2api` runs, then it renders valid manifests for app, PostgreSQL, Redis, services, PVCs, and secrets without requiring manual values.
- Given `postgresql.enabled=false` and `redis.enabled=false` with external connection values, when Helm renders, then bundled PostgreSQL and Redis workloads are absent and app env points at the configured external hosts.
- Given existing secret names are configured, when Helm renders, then chart-managed secret data for those credentials is not emitted and workloads reference the named secret keys.
- Given ingress is enabled with at least one host, when Helm renders, then a v1 Ingress routes to the app service on port 8080.
- Given persistence is disabled, when Helm renders, then the relevant volume uses `emptyDir` and no PVC for that component is emitted.

## Spec Change Log

## Design Notes

Keep bundled PostgreSQL and Redis as simple in-chart Kubernetes workloads instead of third-party chart dependencies. This keeps the deployment artifact local, reviewable, and aligned with the current Compose defaults while still allowing operators to disable bundled dependencies for managed services.

## Verification

**Commands:**
- `helm lint deploy/helm/sub2api` -- expected: chart lint succeeds.
- `helm template sub2api deploy/helm/sub2api` -- expected: default chart renders without YAML/template errors.
- `helm template sub2api deploy/helm/sub2api --set postgresql.enabled=false --set externalPostgresql.host=db.example.com --set externalPostgresql.password=secret --set redis.enabled=false --set externalRedis.host=redis.example.com --set externalRedis.password=secret` -- expected: renders app-only deployment without bundled PostgreSQL/Redis resources.
- `helm template sub2api deploy/helm/sub2api --set ingress.enabled=true --set ingress.hosts[0].host=sub2api.example.com --set ingress.hosts[0].paths[0].path=/ --set ingress.hosts[0].paths[0].pathType=Prefix` -- expected: renders a networking.k8s.io/v1 Ingress.

## Suggested Review Order

**Chart Surface**

- Operator-facing defaults
  [`values.yaml:7`](../../deploy/helm/sub2api/values.yaml#L7)

- Chart identity metadata
  [`Chart.yaml:1`](../../deploy/helm/sub2api/Chart.yaml#L1)

**Secrets And App Wiring**

- Stable generated secrets
  [`secret.yaml:1`](../../deploy/helm/sub2api/templates/secret.yaml#L1)

- Application environment map
  [`deployment.yaml:1`](../../deploy/helm/sub2api/templates/deployment.yaml#L1)

- App data persistence switch
  [`pvc.yaml:1`](../../deploy/helm/sub2api/templates/pvc.yaml#L1)

**Bundled Dependencies**

- PostgreSQL workload parity
  [`postgresql.yaml:1`](../../deploy/helm/sub2api/templates/postgresql.yaml#L1)

- Redis password-aware startup
  [`redis.yaml:1`](../../deploy/helm/sub2api/templates/redis.yaml#L1)

**Exposure And Docs**

- Optional v1 ingress
  [`ingress.yaml:1`](../../deploy/helm/sub2api/templates/ingress.yaml#L1)

- User-facing Helm workflow
  [`README.md:254`](../../deploy/README.md#L254)
