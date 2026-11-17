# api-recon — Reference

Grep recipes, the `config.json` template, and troubleshooting. All greps run against the `js/` dir. If the bundle is single-line, run `js-beautify` or `sed 's/}/}\n/g'` first; usually a raw grep with a context window is enough.

## Script Notes

Every file in `scripts/` is a **reference template** and must be adapted to the target site before running. Typical edits:

| Script | Knobs commonly needing adjustment |
|---|---|
| `harvest_static.py` | endpoint regexes, webpack/Vite manifest parsing, micro-frontend publicPath, retry/concurrency |
| `runtime_harvest.js` | neutralize field names and success values, stub matching rules and body shape, routes source, WS recording, `waitUntil`/`routeTimeout`/`proxy` |
| `preload.js` | `loginPathRe`, L1 stubs, `neutralize.fields`, `apiPattern`, whether L3 is enabled, `recordDetail`, `observe.*`, `neutralizeVueRouter` |
| `spider_mpa.py` | `--exclude` destructive links, cookie, depth/max, same-origin filter |
| `extract_route_map.py` | `routeMap` / `routeLink` regexes, KEY naming pattern |
| `build_perm_tree.py` | `userRouteAuth` parsing, `ROOTS`/`PREFIX_PARENT` hierarchy heuristics, stub outer field names |
| `config.json` | single entry point for every site-specific knob above |

Keep adjusted copies in the task working dir (e.g. `recon/`) and state the exact deviations from the reference scripts in the report.

---

## A. The Three Recon Gates

### A1. Render gate — "How do we tell we are logged in?"

```bash
grep -rhoaE '.{0,40}(isLogin|isAuthenticated|loggedIn|hasLogin|requireAuth)\b.{0,80}' js | head
grep -rhoaE 'function (getUser|getToken|getAuth)[0-9]?\([^)]*\)\{.{0,200}' js | head
grep -rhoaE '(localStorage|sessionStorage)\.getItem\("[^"]+"\)' js | sort -u
grep -rhoaE '(Cookies?|cookie)\.(get|load)\("[^"]+"\)' js | sort -u
grep -rhoaE '\batob\(|JSON\.parse\(|jwt|decode' js | head
```

Follow the chain `isLogin = f(getUser())` -> `getUser = decode(storage.read(KEY))`; pin down the **storage key**, the **container** (Cookie vs localStorage), and the **encoding**:

| Encoding | How to fake it in config |
|---|---|
| Plaintext string / `"1"` / token | `"value": "anything-truthy"` |
| `JSON.parse(x)` | `"value": "json:{\"id\":1,\"username\":\"admin\"}"` |
| `JSON.parse(atob(x))` | `"value": "b64json:{\"id\":1,\"username\":\"admin\"}"` |
| JWT | an unsigned / `alg:none` JWT, or sign with a key found in the bundle |
| Encrypted (SM2/AES/RSA) | find hardcoded keys; forgeable when the render gate only needs a decodable blob; otherwise fall back to static |

-> write into `cookies` / `localStorage`.

### A2. Interceptor gate — "What triggers the jump to /login?"

```bash
grep -rhoaE '.{0,60}(interceptors\.response|axios|request\.use).{0,120}' js | head
grep -rhoaE '.{0,40}(response_code|errcode|errno|\bcode\b|\bret\b|\bstatus\b)\s*[=!]==?\s*[\-0-9]{1,4}.{0,60}' js | head -20
grep -rhoaE '.{0,40}(not logged in|please log in again|session expired|unauthorized|login invalid|token.{0,10}invalid).{0,40}' js | head
grep -rhoaE '.{0,30}(location\.href|router\.(push|replace)|navigate)\([^)]*login[^)]*\)' js | head
```

Pin down the **field name**, the **success value** (usually `0` or `200`), and the **failure values that trigger the jump**. Verify with a junk session:

```bash
curl -sk -X POST -H 'Cookie: <fakekey>=junk' https://target/api/<protected> -d '{}' -H 'Content-Type: application/json'
```

-> write into `neutralize.fields` + `neutralize.success`.

### A3. Content gate — "Where do menus/permissions come from?"

```bash
grep -rhoaE '"/api[^"]*(permission|perm|role|menu|acl|resource|nav)[^"]*"' js | sort -u
grep -rhoaE '.{0,30}(menus|permissions|menuList|routeList|authList|role_permissions)\b.{0,120}' js | head
grep -rhoaE 'userRouteAuth|getResultTree|routeMap|routeLink|hasPermission|checkAuth' js | head
grep -rhoaE '([A-Z_][A-Z0-9_]*):\{name:"[^"]*",link:"/[^"]+"\}' js | head
```

**Two data layers** (typical enterprise admin):

| API | Typical payload | Consumer |
|---|---|---|
| `.../role_permissions` | `{ permissions: string[], role_type }` | route guards, button-level ACL |
| `.../permissions/all` | `tree[{ code, position, children }]` | sidebar menu rendering |
| in-bundle `userRouteAuth` | `{ CODE: { url, name? } }` | code -> frontend path |
| in-bundle `routeMap` | `{ KEY: { name, link } }` | alias resolution (webpack `o.DASHBOARD`) |

Read the consumer code to confirm how `getResultTree(tree, permissions)` filters and which field `v-if` / `hasAuth(code)` checks.

**Manual forge** (small sites): build a permissive payload -> `stubs`.

**Full permission-tree rebuild** (large sites where the sidebar/submodules stay blank): see **section I**.

---

## B. config.json Template

```json
{
  "baseUrl": "https://target/",
  "runtimeMode": "both",
  "chromium": "/usr/bin/chromium",

  "cookies": [
    { "name": "auth", "value": "b64json:{\"id\":1,\"username\":\"admin\",\"role\":\"admin\",\"func\":{},\"permissions\":[\"*\"]}" }
  ],
  "localStorage": { "token": "faketoken", "isLogin": "1" },

  "neutralize": {
    "fields": ["response_code", "code", "errno", "ret", "status"],
    "success": 0,
    "flags": { "success": true, "message": "ok" }
  },
  "forward": true,
  "loginUrlPattern": "/login",
  "apiPattern": "/api/|/rest/|/graphql",

  "mockTier": "L1+L2",
  "recordDetail": true,
  "observe": {
    "storageReads": false,
    "cookieReads": false,
    "xhrHeaders": true
  },
  "neutralizeVueRouter": true,
  "stubs": [
    {
      "match": "permissions/all|/menu|role_permissions",
      "body": {
        "response_code": 0, "code": 0,
        "data": {
          "permissions": ["*"],
          "menus": [
            { "name": "dashboard", "path": "/dashboard", "show": true, "children": [] },
            { "name": "alert", "path": "/alert", "show": true, "children": [] }
          ]
        }
      }
    }
  ],

  "explore": {
    "clickTabs": true,
    "clickTables": true,
    "pushStateFallback": true,
    "maxMenuItems": 50
  },

  "routes": ["/dashboard", "/alert", "/asset", "/device", "/report", "/config", "/system"],
  "waitMs": 1500, "perRouteMs": 900, "headless": true,
  "waitUntil": "domcontentloaded",
  "routeTimeout": 12000,
  "proxy": "",

  "captureResponses": true, "recordWs": true, "respMax": 600
}
```

Field notes:
- `runtimeMode`: `depth` (Puppeteer), `coverage` (browser MCP), `both`
- `cookies[].value` prefixes: `b64json:` -> base64(JSON); `json:` -> raw JSON; no prefix -> literal
- `forward: true` forwards real requests and rewrites the code fields; `false` = fully offline stubs
- `mockTier`: tiers enabled in the coverage-mode preload, e.g. `L1+L2`, `L1+L2+L3`
- `routes` comes from `routes.txt`; after menu forging the harness auto-appends `<a href>`
- `captureResponses` / `recordWs` only apply in depth mode
- `waitUntil`: use `domcontentloaded` on large SPAs to avoid `networkidle2` hangs
- `routeTimeout`: per-route `page.goto` timeout (ms)
- `proxy`: Puppeteer `--proxy-server`; alternatively set `HTTP_PROXY` / `HTTPS_PROXY`

### B1. Dual stub template (role_permissions + permissions/all)

```json
"stubs": [
  {
    "match": "role_permissions",
    "body": {
      "response_code": 0,
      "data": {
        "permissions": ["MONITOR", "MONITOR_ALERT", "THREAT", "ASSETS_RISK"],
        "role_type": "SUPER_ADMIN"
      }
    }
  },
  {
    "match": "permissions/all",
    "body": {
      "response_code": 0,
      "data": [
        {
          "code": "MONITOR",
          "position": 1,
          "children": [
            { "code": "MONITOR_ALERT", "position": 1, "children": [] }
          ]
        }
      ]
    }
  }
]
```

The outer field names (`response_code` / `code` / `data`) must match the A2 interceptor gate; `permissions` must cover every leaf code in the tree.

---

## C. coverage Mode: preload Config

Edit the `CONFIG` object at the top of `scripts/preload.js`, or swap it before CDP injection:

```javascript
const CONFIG = {
  loginPathRe: /\/(login|signin)(\/|$|\?)/i,
  mockTier: 'L1+L2',
  forward: true,
  recordDetail: true,
  extractUrlsFromResponse: true,
  neutralizeVueRouter: true,
  observe: { storageReads: false, cookieReads: false, xhrHeaders: true },
  neutralize: { fields: ['response_code', 'code'], success: 0 },
  stubs: [ /* same as the config.json stubs */ ],
  apiPattern: /\/(api|apis|v\d+|dev|internal|graphql)\//i,
};
```

Verify: `window.__API_RECON_PRELOAD__ === true` and the pathname is stable.

Export the recordings:

```javascript
JSON.stringify({
  apis: [...window.__API_RECON_LOG__],
  detail: window.__API_RECON_DETAIL__,
  routes: [...(window.__API_RECON_ROUTES__ || [])],
  observe: window.__API_RECON_OBSERVE__,
}, null, 2)
```

---

## D. preload / runtime Hook Capabilities

Browser hook capabilities built into preload (coverage) and runtime_harvest (depth), and their coverage:

| Hook | Value for API discovery | Covered |
|---|---|---|
| Hook fetch / XHR.open | record request URL/method | ✅ `recordDetail` + `__API_RECON_LOG__` |
| Hook XHR.setRequestHeader | discover Authorization and other headers | ✅ `observe.xhrHeaders` |
| Hook localStorage/cookie reads | confirm session key names | ⚠️ optional `observe.storageReads/cookieReads` |
| Vue route access | complete frontendRoutes | ✅ `__API_RECON_ROUTES__` (loaded routes) |
| Vue guard neutralization / login-jump blocking | open up modules to trigger APIs | ✅ `neutralizeVueRouter` + native navigation neutralized |
| React route access | complete routes | ⚠️ static + clicks; no dedicated hook |
| Page navigation blocking (login path) | stay on the page to analyze | ⚠️ blocks only the login path, business navigation untouched |
| Hook crypto libs (CryptoJS/SM etc.) | encrypted params -> plaintext API body | ❌ hook the crypto function inputs manually; record the finding in config |
| Anti-debug bypass | otherwise runtime records no APIs | ❌ handle manually; static still works |

---

## E. Endpoint Extraction Regexes (when the static count is too low)

Loosen `extract_endpoints` in `harvest_static.py`, or do it by hand:

```bash
grep -rhoaE '"/[a-z][A-Za-z0-9_/\-]{3,}"' js | sort -u
grep -rhoaE '/api/[a-zA-Z0-9_./-]+' js | sort -u
```

---

## F. Troubleshooting

| Symptom | Cause -> fix |
|---|---|
| Very few static APIs | endpoint dialect mismatch -> loosen the regexes (section D) |
| chunk count ≪ manifest | CSS-only or undeployed chunks; 404s already retried |
| runtime still shows the login page | render gate wrong -> recheck A1: key name, container, encoding, domain |
| Inside the shell but modules blank | content gate -> forge the menus (A3); the `routes` paths may be wrong |
| Every route only bootstrap/locale | permission codes incomplete -> section I permission-tree rebuild; check the `role_permissions` + `permissions/all` dual stub |
| Sidebar item present but subpage blank | the tree is missing intermediate nodes or codes disagree with `userRouteAuth` |
| Every API jumps to login | interceptor gate -> verify `neutralize`; nested fields need a deeper walk |
| 0 WS frames | subscribe happens only after user interaction; raise `perRouteMs` |
| Empty response bodies | real responses exist only with `forward: true` |
| Chromium missing | install chromium or set `config.chromium` / `CHROMIUM` |
| Many mocks yet back to login | hooks too late or the `location.href` setter is missing -> document-start + preload |
| Lists all empty | L3 empty arrays are normal; keep clicking tabs/settings/detail |
| Redux action mistaken for a route | filter internal paths containing get/set/change/clear/toggle/upload |
| Vue still jumps to login | preload is not document-start -> fix the injection timing; or clear the guards manually with `neutralizeVueRouter: false` |
| URL in a response but not in the log | enable `extractUrlsFromResponse`; or extract it by hand from `__API_RECON_DETAIL__` |
| Unknown Authorization header name | enable `observe.xhrHeaders` or inspect request headers in DevTools |
| runtime very slow / timeouts | switch `waitUntil: domcontentloaded`; lower `routeTimeout`; avoid `networkidle2` |
| Proxy connection failed | check `proxy` / env vars; Puppeteer and curl proxy ports must match |

---

## G. Hardened Targets

When the server validates the session step by step (unforgeable signed cookies, server-rendered menus that cannot be stubbed), runtime stalls at the shell. Expected behavior:

- **Static suffices for endpoint enumeration** -- module paths live in the code
- If scope allows, run the same harness with a **real session**: `forward: true`, no neutralize, capture the real methods/params/responses

---

## H. Single-Run Checklist

1. Confirm the authorized scope
2. **Read** `scripts/harvest_static.py` -> adapt to the target -> run -> review `api_static.txt`, `routes.txt`
3. **Phase 1b**: path anchor window expansion + binding layer -> `param_candidates.json` (section J)
4. Reverse A1/A2/A3 -> write the site-specific `config.json`
5. **Read and adapt** `runtime_harvest.js` / `preload.js` before running
6. `runtimeMode=depth`: `npm install` -> run the adapted harvest script
7. `runtimeMode=coverage/both`: document-start inject the adapted preload -> browser MCP dynamic enumeration + the **param trigger matrix**
8. Modules do not render -> **section I permission-tree rebuild** -> patch stubs -> rerun
9. Multi-sample param diff + error back-solving -> `params_merged.json`
10. Merge -> `site_map.json` + `api_merged.txt`; honestly note coverage, gaps, and script deviations

---

## I. Permission Tree Rebuild (Phase 4 Deep-Dive)

For when forging a simple `menus: [{ path, show: true }]` fails and submodules still do not mount.

### I1. Locate the auth module

```bash
grep -l 'userRouteAuth' js/*.js
grep -l 'routeMap\|routeLink' js/*.js
grep -rhoaE 'getResultTree|role_permissions|permissions/all' js | head
```

Record: the **permission API path**, the **response field names**, and the **consuming chunk filename**.

### I2. Extract routeMap

```bash
python3 scripts/extract_route_map.py recon/js recon/
# emits recon/route_map.json
```

If `[!] no routeMap pattern found`: loosen the regexes in `extract_route_map.py`, or grep by hand:

```bash
grep -rhoaE '([A-Z_][A-Z0-9_]*):\{name:"[^"]*",link:"/[^"]+"\}' js | head -20
```

### I3. Build the permission tree + stub

```bash
python3 scripts/build_perm_tree.py recon/js recon/ --config recon/config.json
```

Script logic:
1. Parse `userRouteAuth={MONITOR:{url:...},...}` (including webpack aliases like `He=o.DASHBOARD`)
2. Resolve alias -> real path via `route_map.json`
3. Infer parents from code prefixes (`MONITOR_ALERT` -> `MONITOR`)
4. Emit `permissions_tree.json`, `permissions_all_stub.json`, `role_permissions_stub.json`
5. With `--config`, auto-write into `config.json`'s `stubs` and extend `routes`

**Target-specific knobs** (top of the script):
- `DEFAULT_ROOTS`: top-level module codes
- `DEFAULT_PREFIX_PARENT`: `PREFIX_` -> parent map
- `DEFAULT_EXTRA_PARENT`: orphan nodes with no prefix relation

### I4. Verify stub consistency

```bash
# the permissions count should be ~= the userRouteAuth entry count
wc -l recon/perm_codes_all.txt
# routes must cover every link in route_map
python3 -c "import json; m=json.load(open('recon/route_map.json')); r=set(json.load(open('recon/config.json'))['routes']); print('missing', [v['link'] for v in m.values() if v['link'] not in r])"
```

### I5. Rerun runtime and compare

```bash
node recon/runtime_harvest.js recon/config.json
# compare the runtime_api.json entry counts before/after forging; check whether /attack, /asset, etc. gain module APIs
```

| Before forge | After forge (success) |
|---|---|
| the same 3-5 bootstrap calls on every route | different routes trigger different module APIs |
| only `/api/locale/language` | `/api/web/...` module endpoints appear |
| a handful of `routes.txt` entries | `routes` at 80-110+ sourced from route_map |

### I6. When It Still Fails

- **coverage mode**: click the sidebar + tabs; permission gating may only request after interaction
- **stub fields**: compare the real API (curl + real session) nesting against the stub
- **extra guards**: grep button-level checks like `hasPermission|checkRole|func.` and extend `role_permissions.permissions`
- **static fallback**: the module API paths remain in `api_static.txt`; runtime only adds METHOD/body; keep `param_candidates.json` + the recorded samples for params

---

## J. Parameter Reversing (Phase 1b / 5b / 5c)

**A methodology, not a universal script.** Find paths with regexes; find params via anchor window expansion + UI binding chains + multi-sample diff + error back-solving.

### J1. Anchor Window Expansion — find the payload builder from the path

```bash
# anchor on a path already known from Phase 1
grep -n '"/api/user/list"' js/*.js
grep -rhoaE '.{0,120}("/api[^"]+").{0,200}' js | head
grep -rhoaE '(params|data|body|payload)\s*:\s*\{' js | head
grep -rhoaE '(get|post|put|delete|patch)\([^,]+,\s*\{' js | head
```

### J2. Wrapper Layers and Transport Shapes

```bash
# axios / unified request
grep -rhoaE '(axios|request)\.(get|post|put|delete|patch)\(' js | head
grep -rhoaE 'interceptors\.(request|response)' js | head

# GraphQL
grep -rhoaE '(query|mutation)\s+\w+|gql`|graphql\(' js | head
grep -rhoaE '\$[a-zA-Z_]+\s*:\s*(Int|String|Boolean|\[)' js | head

# FormData / multipart
grep -rhoaE 'FormData|\.append\(' js | head

# path params
grep -rhoaE 'path:\s*"/[^"]*:[^"]+"' js | head
grep -rhoaE 'useParams|route\.params|\$route\.params' js | head
```

### J3. Validation Gate — required / format / enum

```bash
grep -rhoaE '(required|message|pattern|enum|validator)\s*:' js | head
grep -rhoaE 'yup\.|zod\.|async-validator|Form\.Item|a-form-item|el-form-item' js | head
grep -rhoaE 'rules\s*:\s*\[|name:\s*["\'][a-zA-Z_]+["\']' js | head
grep -rhoaE 'label.*value|options\s*:\s*\[' js | head
```

### J4. Binding Layer — form -> API

```bash
grep -rhoaE 'onFinish|handleSubmit|getFieldsValue|validateFields' js | head
grep -rhoaE '(pick|omit|transform|dayjs|moment)\(' js | head
```

runtime fills the gap: DevTools -> Network -> request -> **Initiator** (call stack); walk up from `fetch`/`send` to the payload-building function.

### J5. Encrypted Params

```bash
grep -rhoaE 'encrypt|decrypt|sign|CryptoJS|sm2|sm3|sm4|RSA|AES' js | head
```

**Never guess fields from ciphertext** — hook the crypto function's **inputs** and record the plaintext payload before encryption; record conclusions in `config.json` / `param_candidates.json`.

### J6. Param Trigger Matrix (required in Phase 3)

Per module, record once per action and diff the request body/query:

| Action | Watch |
|---|---|
| List first paint | pagination defaults |
| Search | keyword, filters |
| Advanced filter | optional fields |
| Create/edit | full entity |
| Bulk/export | `ids[]`, `exportType` |
| Sort/page | `sortField`, `order` |

Output `param_samples.json`: `[{ "path", "method", "action": "search", "body", "query", "headers" }]`

### J7. Confidence Rules

| Confidence | Condition |
|---|---|
| **High** | static callsite + >=2 agreeing runtime samples |
| **Medium** | static only, or a single runtime sample |
| **Low** | inferred from responses/errors, not re-verified |
| **Pending trigger** | field known statically; UI/permission never reached it |

### J8. Scenario Quick Recipes

| Scenario | Order |
|---|---|
| REST list page | J1 payload builder -> J6 four-way diff -> J3 rules |
| Create/edit form | J3 Form name -> J4 submit chain -> runtime submit + deliberately leave fields blank for a 400 |
| GraphQL | J2 variables declaration -> have runtime record the variables of every operation |
| Encrypted body | J5 hook the inputs -> the pre-encryption fields are the real params |

### J9. Mapping to the api-recon Phases

| api-recon | Param recon |
|---|---|
| Phase 1 static | J1 anchor window expansion |
| Phase 2 A2 interceptor | globally injected fields (tenantId, sign) |
| Phase 3 runtime | J6 trigger matrix + `param_samples.json` |
| Phase 4 permission tree | different modules, different forms -> full fields only once permissions suffice |
| Phase 5 merge | `params_merged.json` + confidence; never settle required/optional from a single sample |

### J10. Troubleshooting

| Symptom | fix |
|---|---|
| Field name is static-only, never appears at runtime | mark it "pending trigger"; complete the permission tree / click advanced filters / exercise every cascading select option |
| Same path, different body shapes | normal — record one entry per `action`; never force-merge schemas |
| Stub response is fake but the params matter | **read the outbound request** body/headers; never infer from the stub response |
| 400 on a nested field | mind the outer wrappers `data`/`bizData`/`variables` |
| GraphQL shows only operation names | expand the `variables` JSON; grep `$var: Type` statically |

---
