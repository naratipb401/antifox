---
name: api-recon
description: Invoke this skill when collecting a website's API endpoints.
---

# API Recon (Frontend API Recon)

Given **authorization**, discover as completely as possible: **backend APIs** (paths, methods, params, response bodies), **frontend routes**, and **UI trigger points** (tabs, modals, table actions, etc.).

---

## Boundaries and Prohibitions (Agent Must-Read · Violations Leave the Scope)

This skill **only does API / parameter-surface recon**; it is not the vulnerability-hunting or penetration-exploitation phase.

### Task Boundaries

| Scope | Allowed | Forbidden |
|---|---|---|
| **Target** | Enumerate paths, methods, params, routes, UI trigger points | SQLi/XSS/authz-bypass/brute-force/fuzz vulns, request-tampering attacks, destructive ops |
| **Auth** | Hook + stub/mock to bypass the **client-side** login gate | Asking the user for or guessing credentials; attempting real login form submits |
| **Runtime** | Hook APIs without credentials; use mock responses to push the SPA into the logged-in shell | Flows that cannot proceed without a real backend session |

### Credential-Free Dynamic Analysis (Phase 3 Default)

1. Use `preload.js` / `runtime_harvest.js` to **intercept and stub** bootstrap APIs (login, permissions, menus, etc.);
2. Return mock bodies for business query APIs with a **correct structure, success business code, and possibly empty data**;
3. Let the frontend keep rendering logged-in pages with no backend or under 401s, triggering more XHR/fetch/WebSocket;
4. **Empty data, blank tables, and placeholder UI are all expected** -- do not switch to real login or vulnerability testing because of them.

**In one sentence**: use mocks to prop open frontend routes and component mounts, **recording only outbound requests**; what the backend returns does not matter -- what matters is which APIs the frontend **still calls**.

### Hard Process Prohibitions

| Prohibited | Do instead |
|---|---|
| grep/curl/Read the main entry `index-*.js` to extract API paths before Phase 1 completes | Run `OUTDIR/harvest_static.py` |
| Hand-writing `extract_apis.py` or other harvest replacements | Edit `OUTDIR/harvest_static.py` and rerun |
| Repeating the same grep/command after >=2 failures | Change tactic: read tool_logs, adjust harvest, check reference |
| Skipping gate A/B and running the original `scripts/` as-is | Copy to OUTDIR and adapt to the target |
| Real usernames/passwords, OTP, OAuth, or other real auth | stub/mock (see above) |
| Skipping stubs in the name of "getting real data" and doing authz-bypass/injection tests | Record outbound only; that is the recon boundary |
| Irreversible ops: deletion, sensitive-data export, bulk writes, etc. | Same applies to coverage clicks |
| Claiming all pages/APIs were found without finishing runtime + dynamic enumeration | See "Definition of Done" or note the limitations |
| Claiming all params are known without the param trigger matrix + diff | Phase 3b matrix + Phase 5 diff |
| Inferring required/optional from a single runtime sample | Multi-sample diff, or infer from validation rules/errors |

---

## Two-Layer Model + Run Modes

| Layer | Output | Limits |
|---|---|---|
| **Static** (JS bundle) | Full endpoint paths, route draft, callsite field candidates | No HTTP methods; params need Phase 1b; misses runtime-concatenated URLs |
| **Runtime** (live session) | Method + body + response + dynamic URLs + WS/SSE; multi-sample diff completes params | Pages must actually render to fire requests; a single sample cannot settle required/optional |

| Run mode | Engine | Use when |
|---|---|---|
| **depth** | `runtime_harvest.js` (Puppeteer) | API inventory, METHOD/params/response bodies, WS/SSE, reproducible batch runs |
| **coverage** | browser + `preload.js` | Click tabs/modals/tables; deeper feature-point coverage |
| **both** | depth first, then coverage | Most complete, slowest |

**Param methodology** (no universal script): paths via harvest/regex; params via **anchor window expansion + UI binding chains + multi-sample diff + error back-solving** (grep recipes in [reference.md](reference.md), section J).

---

## Definition of Done

Claim recon done only when all of the following hold:

- [ ] **Static**: Phase 1 harvest produced `api_static.txt`, `routes.txt`, `js/`
- [ ] **Runtime**: at least one of depth/coverage; coverage/both require **working hooks + the dynamic enumeration loop**
- [ ] **Inside the shell**: business paths land beyond `/login` (watch hash routing)
- [ ] **Params**: coverage/both completed the param trigger matrix + `param_samples.json`; Phase 5 merged `params_merged.json`
- [ ] **Depth** (if module pages are blank): Phase 4 permission-tree rebuild and rerun until **module-level APIs** appear (not just locale/bootstrap)
- [ ] **Delivery**: all Phase 5 outputs present (see the Phase 5 outputs table); `insert_assets` writes service and endpoint assets

---

## Scripts and Gates

`scripts/` are reference templates only; **never** run the originals as-is and treat the result as final.

**Rule**: read first -> adapt to the target -> write into `OUTDIR` (e.g. `recon/`) -> log `CHANGES.md`; if a script does not fit, rewrite it per the methodology and borrow only the structure.

| Gate | When | Reference script -> OUTDIR copy | Typical required edits |
|---|---|---|---|
| **A (static)** | After Phase 0, **before the first** harvest/spider run | `harvest_static.py` / `spider_mpa.py` | **The default regexes run as-is on most sites**; only on manifest/dialect mismatch adjust the endpoint regex, webpack/Vite `publicPath`, MPA exclude/cookie |
| **B (runtime)** | After Phase 2, before running depth/coverage | `runtime_harvest.js` / `preload.js` + `config.json` | Cookie/localStorage keys, neutralize success values, stubs, login regex, api prefix, hash/history |

**Mandatory SPA order** (not swappable; Phase numbers take precedence over "explore first, script later"):

| Step | Must | Must not |
|---|---|---|
| After Phase 0 completes | Next Bash command = `python3 OUTDIR/harvest_static.py <URL> OUTDIR` | curl/grep/Read the main entry `index-*.js` (usually >500KB) |
| Gate A | Copy script -> tweak as needed -> **run immediately** | Hand-extract APIs first, then decide whether to harvest |
| Before Phase 1 completes | Verify outputs with `wc -l`; on 404 fix harvest and retry | Hand-written extract scripts; repeated greps for undownloaded URLs |
| From Phase 1b on | grep only `OUTDIR/js/*.js` | Use the main bundle instead of harvest output |

- ✅ Copy `harvest_static.py` -> (optional) tweak regexes -> **run immediately**
- ❌ curl the main bundle -> grep repeatedly -> write a throwaway extractor -> harvest last
- **MPA**: after Phase 0 the next Bash command = `python3 OUTDIR/spider_mpa.py ...`

---

## Tool and Output Constraints

| Constraint | Rule |
|---|---|
| Large files | Never Read/grep `index-*.js` >100KB into context; batch-process with the OUTDIR scripts |
| grep output | Always `\| head -20` or `-m 5`; keep only path summaries in the conversation, no bundle dumps |
| Verification | Use `wc -l`, `ls \| wc -l`; never Read whole directories |
| Regex probing | Optional, at most once, only on <=50KB chunks or HTML; harvest is the authority for static results |
| reference | Recipes/templates/troubleshooting live in [reference.md](reference.md); never re-inline the full text |

---

## Execution Roadmap

```
Phase 0 classify + OUTDIR
  -> Gate A -> Phase 1 harvest (★ run it now ★)
  -> Phase 1b param reversing
  -> Phase 2 three auth gates -> config.json
  -> Gate B -> Phase 3 runtime + param matrix
  -> Phase 4 permission tree (if needed) -> rerun Phase 3
  -> Phase 5 merge report + insert_assets bulk-insert every discovered service and endpoint API asset; never drop a discovered asset during insertion
```

Check off in order; **do not enter the next Phase until the previous one is done**.

1. [ ] **Phase 0**: probe SPA/MPA; create `OUTDIR` -> [Phase 0](#phase-0--classification)
2. [ ] **Gate A + Phase 1**: copy scripts -> **immediately** harvest -> verify with `wc -l` -> [Phase 1](#phase-1--static)
3. [ ] **Phase 1b**: anchor window expansion + binding layer -> `param_candidates.json` -> [Phase 1b](#phase-1b--parameter-reversing)
4. [ ] **Phase 2**: three auth gates -> `config.json` -> [Phase 2](#phase-2--the-three-auth-gates)
5. [ ] **Gate B**: adapt the runtime scripts -> [Phase 3](#phase-3--runtime)
6. [ ] **Phase 3**: depth / coverage / both; confirm shell entry; param trigger matrix -> `param_samples.json`
7. [ ] **Phase 4** (if needed): permission tree -> patch stubs -> rerun Phase 3 -> [Phase 4](#phase-4--permission-tree-rebuild)
8. [ ] **Phase 5**: merge outputs + report + `insert_assets` -> [Phase 5](#phase-5--merge-and-report)

---

## Phase 0 — Classification

Fetch the entry HTML and **create `OUTDIR`** (never modify the skill's own `scripts/`):

- **SPA**: empty shell + `<div id=app>` + chunks -> Phases 1-5
- **MPA**: SSR + `<form>`, no endpoint bundle -> after Gate A:

```bash
python3 recon/spider_mpa.py <BASE_URL> <OUTDIR> [--cookie "session=..."] [--max 300] [--depth 5] [--exclude "logout|delete|destroy"]
```

Produces `forms.txt`, `links.txt`, `api_inline.txt`. SPA with forms ≈ 0 -> switch to Phase 1.

---

## Phase 1 — Static

Follow [Scripts and Gates](#scripts-and-gates) · [Tool and Output Constraints](#tool-and-output-constraints).

```bash
python3 recon/harvest_static.py <BASE_URL> <OUTDIR>
```

harvest: parse HTML script tags -> webpack/Vite manifest -> download all lazy chunks -> emit `js/`, `api_static.txt`, `routes.txt`, `chunkmap.txt`.

```bash
wc -l OUTDIR/api_static.txt OUTDIR/routes.txt
ls OUTDIR/js | wc -l
```

- chunk count vs manifest: on 404 fix harvest and retry; never hand-curl chunks one by one
- too few lines in `api_static.txt` -> loosen the endpoint regex in the OUTDIR copy and rerun (see reference)

### Phase 1b — Parameter Reversing

Paths come from Phase 1; param fields need their own recon. grep rules in [Tool and Output Constraints](#tool-and-output-constraints).

**Completion bar**: for every important API you can answer -- field names, transport location, type inference, required or not, sample values, confidence.

#### 1b.0 — Transport Shapes

| Shape | Where params live | Look at statically |
|---|---|---|
| REST JSON | body + query | next to the path anchor: `(params\|data\|body)\s*:\s*\{` |
| GraphQL | `variables` | gql templates, `$page: Int` |
| Legacy form | urlencoded | `<form>`, `FormData` |
| File upload | multipart | `FormData.append` |
| Path params | `/user/:id` | route tables + `useParams` / `$route.params` |
| Encrypted/signed | wrapped in `sign`/`data` | Hook the crypto function's inputs (reference section D) |

Output: tag each API with `transport: query|json|form|graphql|encrypted`.

#### 1b.1 — Anchor Window Expansion

Anchor on a known path and widen the window to find the payload-building object:

```bash
grep -n '"/api/user/list"' OUTDIR/js/*.js | head -20
grep -rhoaE '.{0,120}("/api[^"]+").{0,200}' OUTDIR/js/*.js | head -20
grep -rhoaE '(params|data|body|payload)\s*:\s*\{' OUTDIR/js/*.js | head -20
```

| Wrapper layer | Param clues |
|---|---|
| axios instance | `data` / `params` |
| unified request | interceptors inject global fields |
| OpenAPI client | generated method signatures |
| React Query / SWR | the hook's second argument |
| Vue composable | the composable's arguments |

Type residue: `yup`/`zod`/rules, `Form.Item name=`, embedded Swagger.

-> `param_candidates.json`: `{ path, fields[], source: "static-callsite", confidence }`

#### 1b.2 — Binding Layer

```
Form field -> onFinish/handleSubmit -> transform -> API payload
```

| Binding source | Technique |
|---|---|
| Form submit | follow submit -> transform -> API |
| Table search | `getFieldsValue()` -> `params` |
| Route | `:id` / `?tab=` |
| Interceptor | global `tenantId`, pagination, sign |
| Enum selects | `options` -> API enum values |

In DevTools, walk the call stack up from `fetch`/`XHR.send` to the payload-building function.

#### 1b.3 — Three Payload Questions (≠ Phase 2's Three Auth Gates)

| Question | What to answer |
|---|---|
| **Assembly** | where the payload is built, transform traces |
| **Validation** | required, pattern, enum |
| **Transport** | path / query / body / multipart / headers |

While at the interceptor gate (Phase 2), also read the globally injected fields (Authorization, `X-Tenant-Id`, sign).

#### 1b.4 — Handoff to Phase 3

Candidate fields come from the static/binding layers; **required/optional/conditional dependencies** need the Phase 3 param matrix + diff and Phase 5 error back-solving.

---

## Phase 2 — The Three Auth Gates

grep in `OUTDIR/js/` (with `head`), write `config.json` (recipes in reference):

| Gate | Question | Keywords |
|---|---|---|
| **Render gate** | How to tell we are logged in? | `isLogin`, `getToken`, Cookie/localStorage |
| **Interceptor gate** | What triggers the jump to `/login`? | `response_code`, `errno`, axios interceptor |
| **Content gate** | Where do menus/permissions come from? | `menu`, `permission`, `role`, `acl`, `routes` |

Never treat localStorage key names as credentials -- confirm them from chunks/the request chain.

**Exit = Gate B**: conclusions land in `config.json`, then adapt `OUTDIR/runtime_harvest.js` / `preload.js`.

### Phase 2b — API Observation (Optional)

Use the OUTDIR copy of `preload.js` to confirm session key names, Authorization, nested API URLs:

| Config | Yields |
|---|---|
| `recordDetail: true` | `__API_RECON_DETAIL__` |
| `observe.xhrHeaders: true` | header observation |
| `extractUrlsFromResponse: true` | nested APIs inside responses |
| `observe.storageReads/cookieReads: true` | backfill config |
| `neutralizeVueRouter: true` | `__API_RECON_ROUTES__` |

Each coverage pass exports: `__API_RECON_LOG__`, `__API_RECON_DETAIL__`, `__API_RECON_ROUTES__`, `__API_RECON_OBSERVE__`.

---

## Phase 3 — Runtime

Gate B must already be passed; follow [Boundaries and Prohibitions](#boundaries-and-prohibitionsagent-must-read--violations-leave-the-scope) · the credential-free mock policy.

Set `"runtimeMode": "depth" | "coverage" | "both"` in `config.json` (template in reference).

### Hooks and Stubs (shared by depth + coverage)

| Layer | Scope | Purpose |
|---|---|---|
| L1 precise | auth/permission/bootstrap stubs | pass first-screen auth |
| L2 negative fixup | all JSON responses | not-logged-in code -> success |
| L3 fallback | `/api` etc. not matched by L1 | empty success bodies to prop the UI open |

- **depth**: fake auth + `forward` rewriting business codes + `stubs`; walk `routes` (hash/history); emits `runtime_api.json`
- **coverage**: inject `preload.js` at **document-start** (CDP `addScriptToEvaluateOnNewDocument` or a userscript)

Verify: `window.__API_RECON_PRELOAD__` exists; business paths do not bounce back to `/login`.

```bash
cd recon && npm install
node runtime_harvest.js config.json
```

### 3b — coverage Dynamic Enumeration (Required)

1. Main nav/sidebar -- click every item, wait 1-3s for network
2. Tabs -- `role=tab`, `.ant-tabs-tab`
3. Tables -- view/edit/detail on the first row
4. Toolbars -- export, filter, create (**avoid irreversible deletes**)
5. On each module entry -- merge APIs/routes
6. SPA -- controlled `pushState` for paths missing from `routes.txt` (forbidden on MPA)

**Param trigger matrix** (required): per module, record once per action type, then **diff across samples**:

| Action | Params usually extra |
|---|---|
| List first paint | pagination + default filters |
| Search click | keyword, filter |
| Advanced filter | more optionals |
| Create/edit | full entity |
| Bulk/export/sort | `ids[]`, `exportType`, `sortField` |

**Under stubs, outbound bodies/headers stay real** -- trust the requests. Recordings -> `scan_raw.json`, `param_samples.json`, `api_detail.json`.

- **Vue**: `neutralizeVueRouter: true` + document-start preload
- **React**: `routes.txt` + sidebar clicks + `pushState`
- **both**: 3a depth first, then 3b coverage

---

## Phase 4 — Permission Tree Rebuild

**Trigger**: blank module pages / every route fires only bootstrap (e.g. locale) -> the content gate was not passed.

| Symptom | Meaning |
|---|---|
| Shell entered | render + interceptor gates passed |
| Sidebar items missing / blank on click | stub shape or permission codes incomplete |
| Every route the same few APIs | `v-if permission` not satisfied |
| `routes.txt` far smaller than the bundle | must be completed from the auth module |

```bash
grep -rhoaE '"/api[^"]*(permission|perm|role|menu|acl)[^"]*"' OUTDIR/js/*.js | sort -u | head -30
grep -rhoaE 'userRouteAuth|getResultTree|routeMap|routeLink|menuList|authList' OUTDIR/js/*.js | head -20
```

Typical chain: `role_permissions` (flat codes) + `permissions/all` (tree) -> `getResultTree` -> `userRouteAuth[CODE].url`.

```bash
python3 recon/extract_route_map.py recon/js recon/
python3 recon/build_perm_tree.py recon/js recon/ --config recon/config.json
```

Intermediates: `route_map.json`, `userRouteAuth.json`, `permissions_tree.json`, `*_stub.json`, `perm_codes_all.txt`.

Stub checks: the outer `response_code` matches the interceptor gate; flat codes align with the tree; `routes` covers every link in `route_map`.

After updating `config.json`, **rerun Phase 3**. On large SPAs tune `waitUntil`, `routeTimeout`, `perRouteMs` (reference sections A3/I).

---

## Phase 5 — Merge and Report

### Outputs Table

| File | Phase | Contents |
|---|---|---|
| `js/`, `api_static.txt`, `routes.txt`, `chunkmap.txt` | 1 | static bundle and paths |
| `param_candidates.json` | 1b | static param field candidates |
| `config.json` | 2 | three gates + runtime config |
| `runtime_api.json` | 3a | depth detailed recording (incl. WS/SSE) |
| `param_samples.json`, `scan_raw.json`, `api_detail.json` | 3b | multi-samples, click log, detail |
| `route_map.json` etc. | 4 | permission-tree intermediates (if run) |
| `params_merged.json` | 5 | merged param fields + confidence |
| `api_merged.txt` | 5 | `METHOD /path [params] [static\|runtime\|both]` |
| `site_map.json` | 5 | routes, APIs, params, feature points, limitations |
| **insert_assets** | 5 | write every service and endpoint asset into the asset store |

### 5b — Param Merge

Diff from `param_samples.json`; **no universal merge script**. Confidence rules in reference J7 (high/medium/low/pending trigger).

### 5c — Error Back-Solving

Within scope, send incomplete requests to read 400s (**this is param recon, not vulnerability testing**): `field 'x' is required`, enum errors, etc. Mind the `data` wrapper, `variables`, and pre-encryption `bizData`.

The report must state: runtimeMode, static/runtime API counts, param confidence, uncovered modules, and a `CHANGES.md` summary vs the reference scripts.

Suggested `site_map.json` shape:

```json
{
  "site": "https://example.com",
  "runtimeMode": "both",
  "appType": "vue-spa",
  "routeGuardStrategy": ["nav-neutralize", "L1-auth", "L2-patch", "forward"],
  "apisFromStatic": [],
  "apisFromRuntime": [],
  "apis": [],
  "params": [{ "method": "POST", "path": "/api/user/list", "transport": "json", "fields": [] }],
  "frontendRoutes": [],
  "routesVerifiedByClick": [],
  "featuresTriggered": [],
  "limitations": ""
}
```

More fields and grep recipes in [reference.md](reference.md).

---

## General Notes

- **Framework-agnostic**: webpack/Vite/Angular lazy loading behaves the same
- **Transports**: REST/JSON, GraphQL, WebSocket, SSE; gRPC-web out of scope
- **SSR**: client-side fetch is recordable; RSC/Server Actions not fully enumerable
- **Blind spots**: JSVMP, WASM, strict HMAC/mTLS -> static + note the limitations
- **Param blind spots**: conditional cascades, hidden params, WASM packing -> "pending trigger" / "unreachable"
- **Static is the safety net**: when runtime is blocked, static still enumerates endpoints

---

## Additional Resources

- Grep recipes, the `config.json` template, troubleshooting, hooks, param reversing section J, the site_map template: **[reference.md](reference.md)**
- Reference script paths: the table in [Scripts and Gates](#scripts-and-gates)
