# -*- coding: utf-8 -*-
"""qa-matrix.py — 生产力排查三级矩阵（只读）。

路由（后端真身） × 端点（前端 api 层 + infra 动作目录） × 视图（前端消费面） × 权限目录（perm.go）

一次全量比对产出五类台账（对应 docs/qa-sweep-2026-11-06.md §1 判据）：
  D3-1 GHOST        前端调用、后端无对应路由（点了必 404）
  D3-2 METHOD       同路径存在但动词不匹配
  D3-5 DEAD         后端已注册、前端零调用（已扣除 infra-catalog 运行时驱动面）
  D1-3 PERM-GAP     后端用了 perm.go 目录里不存在的权限点
  D2-3 WRITE-NOGATE 视图调用写端点（后端要 <x>:write/delete/execute/cancel/ack/silence）
                    且该视图内没有对应 hasPerm ⇒ 无权限用户「点了必 403」

设计要点（避免"审计出假债"）：
  * 后端路由不止在 controlplane：`internal/**` 各包自带注册（697 个 HandleFunc 注册点），
    handler 体按**同目录**索引解析（跨包同名函数不会串）。
  * 权限点三种落法都取：注册表达式内联字面量（`h.authorize(h.handleAnalyze,"aiops:read")`）、
    注册行后的闭包体、handler 体内（`s.requireProd(w,r,"system:write")`），同包转手深度 ≤3。
  * 前端端点两种来源都取：`api/*.js` 的字面量/简单拼接调用 + `api/infra-catalog/*.js`
    的 `path: () => '...'` 动作目录（InfraConsoleView 的运行时驱动面，静态扫描曾漏判为 DEAD）。

用法（仓库根目录）：python scripts/qa-matrix.py [--tsv 输出目录]
基线自证：路由/端点/视图数低于下限时退出码 1（正则坏了不许静默）。
"""
import io
import os
import re
import sys
import collections

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT_DIR = 'F:/tmp/qa-matrix'
for i, a in enumerate(sys.argv):
    if a == '--tsv' and i + 1 < len(sys.argv):
        OUT_DIR = sys.argv[i + 1]

FLOOR_ROUTES = 550
FLOOR_ENDPOINTS = 380
FLOOR_VIEWS = 25


def rd(p):
    return io.open(p, encoding='utf-8', errors='replace').read()


# ================================================================ 后端（网关代理表）

PROXY_FILE = 'internal/controlplane/svcproxy/proxy.go'


def _brace_groups(src, marker):
    """从 marker 起做一次花括号配对，返回其内顶层 `{...}` 组的列表。"""
    i = src.find(marker)
    if i < 0:
        return []
    i = src.find('{', i)
    depth, start = 0, i
    end = i
    for j in range(i, len(src)):
        c = src[j]
        if c == '{':
            depth += 1
            if depth == 1:
                start = j
        elif c == '}':
            depth -= 1
            if depth == 0:
                end = j
                break
    inner = src[start + 1:end]
    groups, depth, cur = [], 0, None
    for j, c in enumerate(inner):
        if c == '{':
            if depth == 0:
                cur = j
            depth += 1
        elif c == '}':
            depth -= 1
            if depth == 0 and cur is not None:
                groups.append(inner[cur + 1:j])
    return groups


def parse_proxy_rules():
    """网关代理表（TD-60 切流后 /api/v1/<domain>/* 的真身）：public 前缀 → 上游前缀 + 权限点。

    proxy.go 的 Rules / DeviceExtras 是**唯一的公开路由真身**（它们不经过 mux.HandleFunc），
    不解析它会把 autoscaler/portal/device-svc/incident/runbook 等域全判成 GHOST（实测 17 条假幽灵）。
    """
    src = rd(os.path.join(ROOT, PROXY_FILE))
    out = []
    for marker in ('var Rules = []Rule{', 'var DeviceExtras = []Rule{'):
        for g in _brace_groups(src, marker):
            m = re.search(r'PublicPrefix:\s*"([^"]+)"', g)
            if not m:
                continue
            dom = re.search(r'DomainPrefix:\s*"([^"]+)"', g)
            up = re.search(r'UpstreamPrefix:\s*"([^"]+)"', g)
            pem = re.search(r'\bPerm:\s*"([^"]+)"', g)
            rules = []
            for pm in re.finditer(r'\{Method:\s*http\.Method(\w+)([^}]*)\}', g):
                method, rest = pm.group(1).upper(), pm.group(2)
                pr = re.search(r'Perm:\s*"([^"]+)"', rest)
                pp = re.search(r'PathPrefix:\s*"([^"]+)"', rest)
                ps = re.search(r'PathSuffix:\s*"([^"]+)"', rest)
                rules.append({'method': method, 'perm': pr.group(1) if pr else None,
                              'path_prefix': pp.group(1) if pp else None,
                              'path_suffix': ps.group(1) if ps else None})
            out.append({'public': m.group(1).rstrip('/'),
                        'domain': (dom.group(1) if dom else m.group(1)).rstrip('/'),
                        'upstream': (up.group(1) if up else '/api/v1').rstrip('/'),
                        'perm': pem.group(1) if pem else None, 'rules': rules})
    return out


# ================================================================ 后端

FUNC_RE = re.compile(r'^func\s+(?:\([^)]*\)\s*)?([A-Za-z0-9_]+)\s*\(')
REG_RE = re.compile(r'\.(?:HandleFunc|Handle)\(\s*"([^"]+)"\s*,\s*([^\n]*)')
IDENT_RE = re.compile(r'\b([A-Za-z_][A-Za-z0-9_]*)\b')
PERM_LIT_RE = re.compile(r'"([a-z][a-z0-9_]*:[a-z][a-z0-9_]*)"')
GUARD_NAME = (r'(?:requireProd|requirePermission|subsystemAuthorize|authorizeByRoles|'
              r'authorizeByAPIKey|authorize|Authorize|requirePerm|checkPerm)')
GUARD_CALL_RE = re.compile(r'\b\w+\.' + GUARD_NAME + r'\s*\(')
# 只取**守卫调用的实参**里的字面量：handler 体里其它 "x:y" 字面量多是 action/枚举名（如 HBase 的 "ns:table"），
# 拿整个函数体扫会把它们误记成权限点（假阳性来源，已在 2026-11-06 实测到一条）。
GUARD_ARGS_RE = re.compile(r'\b\w+\.' + GUARD_NAME + r'\s*\(([^()]*)')
CALLEE_RE = re.compile(r'\b\w+\.(handle[A-Za-z0-9_]+|dispatch[A-Za-z0-9_]+|createSystemTask)\s*\(')
WRITE_HINT_RE = re.compile(r'http\.Method(Post|Put|Delete|Patch)')
WRITE_PERM_RE = re.compile(r'^[a-z0-9_]+:(write|delete|execute|cancel|ack|silence|rotate|revoke|deploy|apply|restart|scale|sync|run)$')


def norm_path(p, prefix='/api/v1'):
    p = p.split('?')[0].strip()
    p = re.sub(r'\$\{[^}]*\}', '{}', p)
    p = re.sub(r'\{[^}]*\}', '{}', p)
    p = re.sub(r':[A-Za-z_][A-Za-z0-9_]*', '{}', p)
    p = re.sub(r'/+', '/', p)
    if p.endswith('/') and len(p) > 1:
        p = p[:-1]
    if not p.startswith('/api/'):
        p = prefix + ('' if p.startswith('/') else '/') + p
    return p


def split_pattern(raw):
    m = re.match(r'^(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\s+(.*)$', raw.strip())
    if m:
        return m.group(1), raw.strip()[len(m.group(1)):].strip()
    return '*', raw.strip()


def load_backend():
    """返回 (routes, unresolved)。handler 体按目录索引。"""
    pkgs = {}   # dir -> {funcname: body}
    files = []
    # internal（控制面）+ services（微服务：device-svc/gpu-svc/incident-svc/portal-svc/runbook-svc/
    # autoscaler-svc 等各自注册 /api/v1/* 路由，前端经网关直连这些路径——只扫 internal 会把它们误判成 GHOST）
    for top in ('internal', 'services', 'pkg'):
        for base, dirs, fs in os.walk(os.path.join(ROOT, top)):
            dirs[:] = [d for d in dirs if d not in ('testdata', 'proto', 'node_modules', 'vendor')]
            for f in fs:
                if f.endswith('.go') and not f.endswith('_test.go'):
                    files.append(os.path.join(base, f))
    for p in files:
        d = os.path.dirname(p)
        src = rd(p)
        lines = src.split('\n')
        for i, l in enumerate(lines):
            m = FUNC_RE.match(l)
            if m:
                body = '\n'.join(lines[i + 1:])
                end = body.find('\n}\n')
                if end >= 0:
                    body = body[:end]
                pkgs.setdefault(d, {}).setdefault(m.group(1), body)

    routes = []
    unresolved = 0
    for p in files:
        d = os.path.dirname(p)
        src = rd(p)
        lines = src.split('\n')
        for m in REG_RE.finditer(src):
            raw, expr = m.group(1), m.group(2)
            line_no = src[:m.start()].count('\n') + 1
            method, path_raw = split_pattern(raw)
            subtree = path_raw.endswith('/') and len(path_raw.rstrip('/').split('/')) > 1
            path = norm_path(path_raw)
            # 权限点收集（只认守卫调用的实参字面量）
            perms = set()
            for args in GUARD_ARGS_RE.findall(expr):
                perms |= set(PERM_LIT_RE.findall(args))
            guarded = bool(GUARD_CALL_RE.search(expr))
            body_win = '\n'.join(lines[line_no - 1:line_no + 60])
            names = [n for n in set(IDENT_RE.findall(expr)) if n in pkgs.get(d, {})]
            if not names:
                # 闭包体：注册行起 60 行内找守卫
                for args in GUARD_ARGS_RE.findall(body_win):
                    perms |= set(PERM_LIT_RE.findall(args))
                guarded = guarded or bool(GUARD_CALL_RE.search(body_win))
                unresolved += 1
            seen = set()
            stack = list(names)
            while stack:
                nm = stack.pop()
                if nm in seen:
                    continue
                seen.add(nm)
                body = pkgs.get(d, {}).get(nm, '')
                if GUARD_CALL_RE.search(body):
                    guarded = True
                    for args in GUARD_ARGS_RE.findall(body):
                        perms |= set(PERM_LIT_RE.findall(args))
                for callee in CALLEE_RE.findall(body):
                    if callee not in seen:
                        stack.append(callee)
            if method == '*':
                joined = pkgs.get(d, {}).get(names[0], '') if names else body_win
                if WRITE_HINT_RE.search(joined) or WRITE_HINT_RE.search(body_win):
                    method = 'WRITE?'
                else:
                    method = 'ANY'
            rel = os.path.relpath(p, ROOT).replace('\\', '/')
            routes.append({'file': rel, 'line': line_no, 'method': method, 'path': path,
                           'subtree': subtree, 'guarded': guarded, 'perms': perms,
                           'from_service': rel.startswith('services/')})
    return routes, unresolved


# ================================================================ 前端

CALL_RE = re.compile(
    r'\b(getJSON|postJSON|putJSON|deleteJSON|deleteJSONBody|postEmpty|'
    r'http\.(?:get|post|put|delete|patch))\s*\(\s*([\'"`])((?:[^\'"`\\]|\\.)*?)\2')
FN_METHOD = {'getJSON': 'GET', 'http.get': 'GET', 'postJSON': 'POST', 'postEmpty': 'POST',
             'http.post': 'POST', 'putJSON': 'PUT', 'http.put': 'PUT', 'deleteJSON': 'DELETE',
             'deleteJSONBody': 'DELETE', 'http.delete': 'DELETE', 'http.patch': 'PATCH'}
EXPORT_FN_RE = re.compile(r'export\s+(?:const|async\s+function|function)\s+([A-Za-z0-9_]+)')
CONCAT_RE = re.compile(r'([\'"])((?:[^\'"\\]|\\.)*)\1')
CAT_PATH_RE = re.compile(r"path:\s*\(?\)?\s*=>\s*'([^']+)'")
CAT_METHOD_RE = re.compile(r"method:\s*'(\w+)'")


def api_module_functions(path):
    src = rd(path)
    per_fn = collections.defaultdict(list)
    decls = [(m.start(), m.group(1)) for m in EXPORT_FN_RE.finditer(src)]
    decls.append((len(src), None))
    for idx in range(len(decls) - 1):
        start, fname = decls[idx]
        seg = src[start:decls[idx + 1][0]]
        for m in CALL_RE.finditer(seg):
            fn, lit = m.group(1), m.group(3)
            method = FN_METHOD.get(fn)
            if not method:
                continue
            if '${' in lit:
                lit = re.sub(r'\$\{[^}]*\}', '{}', lit)
            per_fn[fname].append((method, norm_path(lit)))
            tail = seg[m.end():m.end() + 240]
            if re.match(r'\s*\+', tail):
                lits = [x[1] for x in CONCAT_RE.findall(tail)]
                if len(lits) >= 2:
                    per_fn[fname].append((method, norm_path('{}'.join(lits))))
        # 动态实参（三元/变量）：把所有出现在实参里的字面量都当候选（放宽召回，避免误判 DEAD）
        for m in CALL_RE.finditer(seg):
            fn = m.group(1)
            method = FN_METHOD.get(fn)
            if not method:
                continue
            tail = seg[m.end():m.end() + 200]
            if re.match(r'\s*[\'"]', tail):
                continue  # 第一实参是字面量，已由上面的分支处理
            for _q, lit in CONCAT_RE.findall(tail):
                if lit.startswith('/'):
                    per_fn[fname].append((method, norm_path(lit)))
    return per_fn


def frontend_endpoints():
    d = os.path.join(ROOT, 'web', 'enterprise', 'src', 'api')
    mods = {}
    for name in sorted(os.listdir(d)):
        if not name.endswith('.js') or name.endswith('.test.js'):
            continue
        mods[name[:-3]] = dict((k, sorted(set(v)))
                               for k, v in api_module_functions(os.path.join(d, name)).items())
    # infra 动作目录：path: () => '...'
    cat = []
    cat_dir = os.path.join(d, 'infra-catalog')
    cat_files = [os.path.join(cat_dir, f) for f in sorted(os.listdir(cat_dir))] if os.path.isdir(cat_dir) else []
    cat_files.append(os.path.join(d, 'infra-ops.js'))
    for f in cat_files:
        if not f.endswith('.js') or f.endswith('.test.js'):
            continue
        src = rd(f)
        lines = src.split('\n')
        for m in CAT_PATH_RE.finditer(src):
            line_no = src[:m.start()].count('\n') + 1
            win = '\n'.join(lines[line_no - 1:line_no + 4])
            mm = CAT_METHOD_RE.search(win)
            cat.append((mm.group(1) if mm else 'POST', norm_path(m.group(1))))
    mods['__infra_catalog__'] = {'_catalog': sorted(set(cat))}
    return mods


IMPORT_RE = re.compile(r'import\s*\{([^}]*)\}\s*from\s*[\'"]@/api/([\w-]+)[\'"]')
STORE_IMPORT_RE = re.compile(r'import\s*\{([^}]*)\}\s*from\s*[\'"]@/stores/([\w-]+)[\'"]')
HASPERM_RE = re.compile(r'hasPerm\(\s*[\'"]([^\'"]+)[\'"]\s*\)')


def stores_endpoints(mods):
    """stores/*.js → 该 store 触及的端点（很多视图不直接 import api，而是走 store）。"""
    d = os.path.join(ROOT, 'web', 'enterprise', 'src', 'stores')
    out = {}
    if not os.path.isdir(d):
        return out
    for name in sorted(os.listdir(d)):
        if not name.endswith('.js') or name.endswith('.test.js'):
            continue
        src = rd(os.path.join(d, name))
        eps = []
        for m in IMPORT_RE.finditer(src):
            mod = m.group(2)
            for fn in (x.strip() for x in m.group(1).split(',')):
                if fn:
                    for method, path in mods.get(mod, {}).get(fn, []):
                        eps.append((mod, fn, method, path))
        if eps:
            out[name[:-3]] = eps
    return out


def views_usage(mods, stores):
    out = {}
    for base, _dirs, files in os.walk(os.path.join(ROOT, 'web', 'enterprise', 'src', 'views')):
        for f in files:
            if not f.endswith('.vue'):
                continue
            p = os.path.join(base, f)
            rel = os.path.relpath(p, ROOT).replace('\\', '/')
            src = rd(p)
            eps, perms = [], set(HASPERM_RE.findall(src))
            for m in IMPORT_RE.finditer(src):
                mod = m.group(2)
                for fn in (x.strip() for x in m.group(1).split(',')):
                    if fn:
                        for method, path in mods.get(mod, {}).get(fn, []):
                            eps.append((mod, fn, method, path))
            for m in STORE_IMPORT_RE.finditer(src):
                mod = m.group(2)
                # auth store 是全局会话单例（几乎每个视图都 import），它顺带 import 了
                # admin 用户/角色 API ⇒ 归因会把 user:write/role:write 扣到全站每个视图上
                # （实测噪声：LoginView/RegisterView/FluxView 都出现「缺 role:write」）。会话面不算写操作消费。
                if mod == 'auth':
                    continue
                for _mod, fn, method, path in stores.get(mod, []):
                    eps.append(('store:' + mod, fn, method, path))
            if 'INFRA_DOMAINS' in src or 'infra-catalog' in src:
                for method, path in mods['__infra_catalog__'].get('_catalog', []):
                    eps.append(('infra-catalog', 'action', method, path))
            if eps or perms:
                out[rel] = {'endpoints': eps, 'hasPerm': perms}
    return out


def perm_catalog():
    src = rd(os.path.join(ROOT, 'internal', 'store', 'model', 'perm.go'))
    return set(re.findall(r'"([a-z][a-z0-9_]*:[a-z][a-z0-9_]*)"', src))


# ================================================================ 主流程

def main():
    routes, unresolved = load_backend()
    mods = frontend_endpoints()
    stores = stores_endpoints(mods)
    views = views_usage(mods, stores)
    catalog = perm_catalog()

    exact = collections.defaultdict(list)
    subtrees = []
    svc_paths = set()
    svc_subtrees = []
    for r in routes:
        if r['subtree']:
            subtrees.append(r)
        exact[r['path']].append(r)
        if r.get('from_service'):
            svc_paths.add(r['path'])
            if r['subtree']:
                svc_subtrees.append(r['path'])
    proxy_rules = parse_proxy_rules()

    fe_by_path = collections.defaultdict(list)
    for mod, fns in mods.items():
        if mod == '__infra_catalog__':
            for method, path in fns.get('_catalog', []):
                fe_by_path[path].append(('infra-catalog', 'action', method))
            continue
        for fn, recs in fns.items():
            for method, path in recs:
                fe_by_path[path].append((mod, fn, method))

    def proxy_hit(path):
        """网关代理真身：/api/v1/<domain>/* → 上游 services 路由（剥域前缀、拼上游前缀）。"""
        for pr in proxy_rules:
            if path == pr['public'] or path.startswith(pr['public'] + '/'):
                tail = path[len(pr['domain']):]
                upstream = norm_path(pr['upstream'] + tail if tail else pr['upstream'] + '/')
                ok = upstream in svc_paths or any(upstream.startswith(s + '/') for s in svc_subtrees) \
                    or any(upstream.startswith(s + '/') for s in svc_paths)
                if ok:
                    perms = set()
                    if pr['perm']:
                        perms.add(pr['perm'])
                    for r in pr['rules']:
                        if r['perm']:
                            perms.add(r['perm'])
                    return [{'file': PROXY_FILE, 'line': 0, 'method': '*', 'path': path,
                             'subtree': False, 'guarded': True, 'perms': perms,
                             'from_service': False, 'proxied': True}]
        return []

    def be_hit(path):
        if path in exact:
            return list(exact[path])
        for r in subtrees:
            if path.startswith(r['path'] + '/'):
                return [r]
        return proxy_hit(path)

    ghosts, methods = [], []
    for path, lst in sorted(fe_by_path.items()):
        hits = be_hit(path)
        if not hits:
            ghosts.append((path, lst))
            continue
        if any(h['subtree'] for h in hits):
            continue
        be_methods = set(h['method'] for h in hits)
        if '*' in be_methods or 'ANY' in be_methods or 'WRITE?' in be_methods:
            continue
        for mod, fn, method in lst:
            if method not in be_methods:
                methods.append((path, method, '/'.join(sorted(be_methods)), mod, fn))

    dead, dead_svc = [], []
    for path, lst in sorted(exact.items()):
        if path in fe_by_path:
            continue
        if any(path.startswith(s['path'] + '/') or path == s['path'] for s in subtrees):
            continue
        if any(p.startswith(path + '/') for p in fe_by_path):
            continue
        row = (path, lst[0]['method'], lst[0]['file'], '')
        # 微服务上游路径：只在前端经网关调用其 public 前缀时才算被消费（逐条按代理表回映）
        if lst[0].get('from_service'):
            mapped = False
            for pr in proxy_rules:
                if path.startswith(pr['upstream'] + '/') or path == pr['upstream']:
                    tail = path[len(pr['upstream']):]
                    pub = norm_path(pr['public'] + tail)
                    if pub in fe_by_path or any(p.startswith(pub + '/') for p in fe_by_path) \
                            or any(pub.startswith(p + '/') for p in fe_by_path):
                        mapped = True
                        break
            if mapped:
                continue
            dead_svc.append(row)
            continue
        dead.append(row)

    perm_gap = []
    for r in routes:
        for pm in r['perms']:
            if pm not in catalog:
                perm_gap.append((pm, r['file'], r['line'], r['path']))

    no_gate = []
    for rel, info in sorted(views.items()):
        need = set()
        for _mod, _fn, _method, path in info['endpoints']:
            for r in be_hit(path):
                for pm in r['perms']:
                    if WRITE_PERM_RE.match(pm):
                        need.add(pm)
        missing = sorted(pm for pm in need if pm not in info['hasPerm'])
        if missing:
            no_gate.append((rel, missing, len(info['endpoints'])))

    os.makedirs(OUT_DIR, exist_ok=True)

    def dump(name, header, rows):
        with io.open(os.path.join(OUT_DIR, name), 'w', encoding='utf-8', newline='\n') as fh:
            fh.write(header + '\n')
            for row in rows:
                fh.write('\t'.join(str(x) for x in row) + '\n')

    total_routes = len(routes)
    total_fe = sum(len(v) for k, v in mods.items() if k != '__infra_catalog__')
    cat_n = len(mods['__infra_catalog__'].get('_catalog', []))
    print('== qa-matrix（静态口径，只读）==')
    print('后端路由：%d（子路径模式 %d，未解析到具名 handler 需靠闭包窗口：%d）' %
          (total_routes, len(subtrees), unresolved))
    print('前端 api 函数：%d ；infra 动作目录端点：%d ；走 store 的 store 数：%d ；视图(有消费或门)：%d ；权限目录：%d' %
          (total_fe, cat_n, len(stores), len(views), len(catalog)))
    print('GHOST(前端有/后端无)=%d ；METHOD 错配=%d ；DEAD(控制面后端有/前端无)=%d ；DEAD(微服务上游无调用)=%d' %
          (len(ghosts), len(methods), len(dead), len(dead_svc)))
    print('PERM-GAP(目录缺权限点)=%d ；WRITE-NOGATE(视图写端点无门)=%d' %
          (len(perm_gap), len(no_gate)))

    print()
    print('-- GHOST 前 30 条 --')
    for path, lst in ghosts[:30]:
        print('  %-56s <- %s' % (path, ','.join('%s.%s:%s' % x for x in lst[:3])))
    print()
    print('-- METHOD 错配 前 20 条 --')
    for row in methods[:20]:
        print('  %-50s 前端=%-6s 后端=%s (%s.%s)' % (row[0], row[1], row[2], row[3], row[4]))
    print()
    print('-- DEAD 分组（前 25 组）--')
    groups = collections.Counter()
    for path, _m, _f, _h in dead:
        groups['/'.join(path.split('/')[:5])] += 1
    for seg, n in groups.most_common(25):
        print('  %-52s %d' % (seg, n))
    print()
    print('-- PERM-GAP（全部）--')
    for pm, f, ln, path in perm_gap[:30]:
        print('  %-28s %s:%s  %s' % (pm, f, ln, path))
    print()
    print('-- WRITE-NOGATE（前 45 条）--')
    for rel, missing, n in no_gate[:45]:
        print('  %-58s 缺=[%s] (端点 %d)' % (rel.replace('web/enterprise/src/', ''), ','.join(missing), n))

    dump('ghost.tsv', 'path\tconsumers', ghosts)
    dump('method.tsv', 'path\tfe_method\tbe_methods\tmodule\tfn', methods)
    dump('dead.tsv', 'path\tmethod\tfile\thandler', dead)
    dump('dead_service_upstream.tsv', 'path\tmethod\tfile\thandler', dead_svc)
    dump('perm_gap.tsv', 'perm\tfile\tline\tpath', perm_gap)
    dump('write_nogate.tsv', 'view\tmissing_perms\tn_endpoints', no_gate)
    print()
    print('TSV → %s' % OUT_DIR)

    bad = 0
    if total_routes < FLOOR_ROUTES:
        print('!! 路由数 %d < 下限 %d' % (total_routes, FLOOR_ROUTES))
        bad = 1
    if total_fe < FLOOR_ENDPOINTS:
        print('!! 前端 api 函数 %d < 下限 %d' % (total_fe, FLOOR_ENDPOINTS))
        bad = 1
    if len(views) < FLOOR_VIEWS:
        print('!! 视图数 %d < 下限 %d' % (len(views), FLOOR_VIEWS))
        bad = 1
    return bad


if __name__ == '__main__':
    sys.exit(main())
