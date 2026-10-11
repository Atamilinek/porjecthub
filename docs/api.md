# API reference

The API is same-origin JSON under `/api/v1`. All routes except login and `/healthz` require the secure session cookie. Every mutation also requires the session's `X-CSRF-Token` and a same-origin `Origin` header. Admin-only routes are not registered outside the authenticated route middleware. Responses use normal HTTP status codes; errors are plain text at present. `X-Request-ID` is returned on every response and accepted from a caller only after validation.

| Method | Path | Role | Purpose |
|---|---|---|---|
| POST | `/login` | Public, Origin checked | Authenticate; returns user and CSRF token; sets secure cookie |
| GET | `/session` | Any authenticated user | Current user and CSRF token |
| POST | `/logout` | Any authenticated user | Revoke current session |
| GET/POST | `/projects` | Viewer+ / Admin mutation | List/search or create projects |
| GET/PUT/DELETE | `/projects/{id}` | Viewer+ / Admin mutation | Read, update, or delete project |
| PUT | `/projects/{id}/favorite` | Any authenticated user | Set current user's favorite state |
| GET/POST | `/categories` | Viewer+ / Admin mutation | List or create categories |
| GET/PUT/DELETE | `/categories/{id}` | Viewer+ / Admin mutation | Read, update, or delete category |
| GET/POST | `/health-checks` | Viewer+ / Admin mutation | List or create checks |
| DELETE | `/health-checks/{id}` | Admin | Delete check |
| GET | `/health-checks/{id}/samples` | Viewer+ | Read bounded sample history |
| GET | `/events` | Viewer+ | Authenticated SSE; replays events after `Last-Event-ID` |
| GET/POST | `/hosts` | Viewer+ / Admin mutation | List or register Agent host mapping |
| DELETE | `/hosts/{id}` | Admin | Delete mapping when no targets depend on it |
| GET/POST | `/service-targets` | Viewer+ / Admin mutation | List or bind host policy target |
| DELETE | `/service-targets/{id}` | Admin | Delete target mapping when unused by checks/jobs |
| GET | `/service-targets/{id}/status` | Viewer+ | Read actual status through Agent |
| POST | `/service-targets/{id}/actions` | Operator+ | Queue start/stop/restart; requires `Idempotency-Key` |
| GET | `/action-jobs` | Viewer+ | List recent persisted jobs |
| GET | `/action-jobs/{id}` | Viewer+ | Read persisted job status |
| GET | `/admin/users` | Admin | List account metadata (never password hashes) |
| POST | `/admin/users` | Admin | Create account with validated role and password |
| PATCH | `/admin/users/{id}` | Admin | Enable/disable; disabling revokes sessions and protects the last admin |
| PUT | `/admin/users/{id}/password` | Admin | Reset password and revoke sessions |
| GET | `/admin/audit` | Admin | Filter and page audit records (`q`, `limit` 1–100, `offset`) |
| GET | `/admin/settings` | Admin | Read effective process settings; changes are made through environment configuration |
| GET | `/healthz` | Internal health probe | Database-backed liveness response |

Roles are hierarchical: Viewer can read, Operator can read and request authorized actions, Admin can manage resources and accounts. The server checks role, CSRF, origin, target mapping, and Agent policy independently. Web visibility is not an authorization boundary.

`/projects` supports `q`, `category_id`, `favorites`, `sort`, `direction`, `limit`, and `offset`. Audit `q` searches actor username, action, event type, and outcome. Agent action bodies contain only a supported action; the target is selected by the path and must have an idempotency header. The API never accepts Docker IDs or systemd unit names in an action request.
