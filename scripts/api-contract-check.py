#!/usr/bin/env python3
"""Check evo's HTTP requests against the official Cloud.ru API specifications.

Prototype of the contract check described in docs/internal/api-contract.md.
It needs the specs that scripts/cloudru-specs.py downloads and the contract
file scripts/api-contract.json, which lists every request evo makes: method,
path, query parameters, body fields, response fields the code reads, enum
values and HTTP statuses the code relies on. For each request it reports:

  path/method   the path and method exist in the spec
  query         every required query parameter is sent; every sent one is
                declared; numeric values respect `maximum`
  body          every required body field is sent; every sent field is
                declared (an error when the schema forbids extra fields)
  reads         every response field the code reads exists in the 2xx schema
  enum          every enum value the code compares against is in the spec
  status        every HTTP status the code handles specially is documented

Findings are `error` (the request contradicts the spec), `warn` (the spec
says nothing, or the code reads a field the spec does not have) or `info`.
Each request may carry "accepted": {"<finding id>": "<why>"} for known,
deliberate deviations (live API behaviour that the spec lacks); those are
reported as accepted and do not fail the run. Exit status is 1 when any
unaccepted error remains, so a changed spec that breaks a request shows up
in CI.

Usage: python3 scripts/api-contract-check.py [--specs .cache/cloudru-specs]
           [--contract scripts/api-contract.json] [--markdown]
"""
import argparse
import fnmatch
import glob
import json
import os
import re
import sys

try:
    import yaml
except ImportError:  # pragma: no cover
    print("PyYAML is required: pip install pyyaml", file=sys.stderr)
    sys.exit(2)

METHODS = ("get", "post", "put", "patch", "delete")


class Spec:
    def __init__(self, path):
        self.path = path
        self.doc = yaml.safe_load(open(path, encoding="utf-8"))
        self.paths = self.doc.get("paths") or {}

    def resolve(self, node, stack=()):
        """Expand $ref recursively; recursive references stop at a stub."""
        if isinstance(node, dict):
            ref = node.get("$ref")
            if isinstance(ref, str) and ref.startswith("#/"):
                if ref in stack or len(stack) > 20:
                    return {"type": "object", "x-recursive": ref}
                cur = self.doc
                for part in ref[2:].split("/"):
                    cur = (cur or {}).get(part.replace("~1", "/").replace("~0", "~"), {})
                return self.resolve(cur, stack + (ref,))
            return {k: self.resolve(v, stack) for k, v in node.items()}
        if isinstance(node, list):
            return [self.resolve(v, stack) for v in node]
        return node

    def find_path(self, path):
        """Match a concrete or templated path; parameter names may differ."""
        if path in self.paths:
            return path
        want = re.sub(r"\{[^}]+\}", "{}", path)
        for p in self.paths:
            if re.sub(r"\{[^}]+\}", "{}", p) == want:
                return p
        return None

    def operation(self, method, path):
        p = self.find_path(path)
        if p is None:
            return None, None, None
        item = self.resolve(self.paths[p])
        op = item.get(method.lower())
        if op is None:
            return p, item, None
        params = list(item.get("parameters") or []) + list(op.get("parameters") or [])
        op = dict(op)
        op["_params"] = params
        return p, item, op


def merge_schema(s):
    """Flatten allOf; return (properties, required, additionalProperties)."""
    if not isinstance(s, dict):
        return {}, set(), None
    props, req, addl = {}, set(), s.get("additionalProperties")
    parts = [s] + list(s.get("allOf") or [])
    for part in parts:
        props.update(part.get("properties") or {})
        req.update(part.get("required") or [])
        if part.get("additionalProperties") is False:
            addl = False
    return props, req, addl


def body_schema(op):
    rb = op.get("requestBody")
    if rb:
        for ct, c in (rb.get("content") or {}).items():
            return c.get("schema") or {}
    for p in op["_params"]:
        if p.get("in") == "body":
            return p.get("schema") or {}
    return None


def success_schema(op):
    for code, r in (op.get("responses") or {}).items():
        if not str(code).startswith("2"):
            continue
        for ct, c in (r.get("content") or {}).items():
            return c.get("schema") or {}
        if "schema" in r:
            return r["schema"]
    return None


def variants(s):
    """A schema or, for oneOf/anyOf, each alternative (allOf is merged later)."""
    if isinstance(s, dict) and (s.get("oneOf") or s.get("anyOf")):
        return list(s.get("oneOf") or s.get("anyOf"))
    return [s]


def walk(schema, dotted):
    """Follow `a.b[].c` through a schema; return the leaf schema or None.

    A top-level array (the v1.1 VM create answers with a list) is entered
    without an explicit `[]`; oneOf/anyOf alternatives are all tried."""
    cur = schema
    segs = dotted.split(".")
    if isinstance(cur, dict) and cur.get("type") == "array" and not segs[0].startswith("[]"):
        cur = cur.get("items") or {}
    for seg in segs:
        is_list = seg.endswith("[]")
        name = seg[:-2] if is_list else seg
        if name:
            found = None
            for v in variants(cur):
                props, _, _ = merge_schema(v)
                if name in props:
                    found = props[name]
                    break
            if found is None:
                return None
            cur = found
        if is_list:
            if cur.get("type") != "array" and "items" not in cur:
                return None
            cur = cur.get("items") or {}
    return cur


def param_schema(p):
    return p.get("schema") or {k: p[k] for k in ("type", "enum", "maximum", "default") if k in p}


def check_request(spec, req):
    out = []  # (level, id, text)

    def add(level, fid, text):
        out.append([level, fid, text])

    method, path = req["method"], req["path"]
    p, item, op = spec.operation(method, path)
    if p is None:
        add("error", "path-missing", f"путь {path} отсутствует в спецификации")
        return out
    if op is None:
        have = [m.upper() for m in item if m in METHODS]
        add("error", "method-missing", f"метод {method} не описан для {p} (есть: {', '.join(have)})")
        return out
    if p != path:
        add("info", "path-param-name", f"в спецификации путь записан как {p}")
    if op.get("deprecated"):
        add("warn", "deprecated", "операция помечена deprecated")

    # --- query ---
    declared = {q["name"]: q for q in op["_params"] if q.get("in") == "query"}
    sent = {}
    for q in req.get("query", []):
        name, _, val = q.partition("=")
        sent[name] = val
    for name, q in declared.items():
        if q.get("required") and name not in sent:
            add("error", f"query-required:{name}", f"обязательный query-параметр `{name}` не передаётся")
    for name, val in sent.items():
        if name not in declared:
            add("warn", f"query-unknown:{name}", f"query-параметр `{name}` не описан в спецификации")
            continue
        sch = param_schema(declared[name])
        if val.lstrip("-").isdigit() and "maximum" in sch and int(val) > sch["maximum"]:
            add("error", f"query-max:{name}", f"`{name}={val}` больше максимума {sch['maximum']}")
        if sch.get("enum") and val and val not in [str(e) for e in sch["enum"]]:
            add("error", f"query-enum:{name}", f"`{name}={val}` вне enum {sch['enum']}")
    if "limit" in declared or "pageSize" in declared or "page_size" in declared or "pageToken" in declared or "page_token" in declared:
        if not req.get("paginates"):
            add("info", "pagination", "ответ постраничный; код читает только первую страницу")

    # --- body ---
    body_sent = req.get("body")
    bs = body_schema(op)
    if body_sent is not None:
        if bs is None:
            add("warn", "body-undeclared", "тело запроса не описано в спецификации")
        else:
            if bs.get("type") == "array":
                bs = bs.get("items") or {}
            props, required, addl = merge_schema(bs)
            sent_top = {f.split(".")[0].rstrip("[]") for f in body_sent}
            for f in required:
                if f not in sent_top:
                    add("error", f"body-required:{f}", f"обязательное поле тела `{f}` не передаётся")
            # One level down: required sub-fields of every object the code sends.
            for top in sorted(sent_top):
                sub = walk(bs, top)
                if not isinstance(sub, dict):
                    continue
                if sub.get("type") == "array":
                    sub = sub.get("items") or {}
                    sent_sub = {f.split(".")[1].rstrip("[]") for f in body_sent if f.startswith(top + "[].") }
                else:
                    sent_sub = {f.split(".")[1].rstrip("[]") for f in body_sent if f.startswith(top + ".")}
                if not sent_sub:
                    continue
                for v in variants(sub):
                    _, sub_req, _ = merge_schema(v)
                    for f in sorted(sub_req):
                        if f not in sent_sub:
                            add("error", f"body-required:{top}.{f}", f"обязательное поле тела `{top}.{f}` не передаётся")
                    break
            for f in body_sent:
                base = f.split(".")[0].rstrip("[]")
                if base not in props:
                    level = "error" if addl is False else "warn"
                    add(level, f"body-unknown:{base}",
                        f"поле тела `{base}` не описано" + (" (additionalProperties: false)" if addl is False else ""))
                elif "." in f or "[]" in f:
                    if walk(bs, f) is None:
                        add("warn", f"body-unknown:{f}", f"поле тела `{f}` не описано")
    elif bs is not None and (op.get("requestBody") or {}).get("required"):
        props, required, _ = merge_schema(bs)
        if required:
            add("error", "body-missing", f"спецификация требует тело с полями {sorted(required)}")

    # --- response fields read ---
    rs = success_schema(op)
    for f in req.get("reads", []):
        if rs is None:
            add("warn", "response-undeclared", "успешный ответ не описан в спецификации")
            break
        if walk(rs, f) is None:
            add("warn", f"read-unknown:{f}", f"код читает поле ответа `{f}`, которого нет в спецификации")

    # --- enums the code relies on ---
    for field, values in (req.get("enums") or {}).items():
        leaf = walk(rs or {}, field) if rs else None
        enum = (leaf or {}).get("enum")
        if not enum:
            add("info", f"enum-undeclared:{field}", f"для `{field}` спецификация не задаёт enum")
            continue
        missing = [v for v in values if v not in enum]
        if missing:
            add("warn", f"enum-unknown:{field}", f"код ждёт значения {missing} в `{field}`, в спецификации их нет (enum: {enum})")

    # --- statuses handled ---
    documented = {str(c) for c in (op.get("responses") or {})}
    for st in req.get("statuses", []):
        if str(st) not in documented:
            add("info", f"status-undocumented:{st}", f"код обрабатывает HTTP {st}, спецификация его не описывает")
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--specs", default=".cache/cloudru-specs")
    ap.add_argument("--contract", default=os.path.join(os.path.dirname(__file__), "api-contract.json"))
    ap.add_argument("--markdown", action="store_true", help="print a markdown table instead of plain lines")
    args = ap.parse_args()

    contract = json.load(open(args.contract, encoding="utf-8"))
    rows, failed = [], 0
    for svc in contract["services"]:
        files = sorted(glob.glob(os.path.join(args.specs, svc["spec"])))
        if not files:
            rows.append((svc["name"], "—", "—", "error", "spec-missing", f"нет файла спецификации {svc['spec']}"))
            failed += 1
            continue
        spec = Spec(files[0])
        for req in svc["requests"]:
            accepted = req.get("accepted") or {}
            findings = check_request(spec, req)
            where = f"{req['method']} {req['path']}"
            if not findings:
                rows.append((svc["name"], where, req.get("code", ""), "ok", "", ""))
            for level, fid, text in findings:
                if fid in accepted or any(fnmatch.fnmatch(fid, a) for a in accepted):
                    why = accepted.get(fid) or next(v for a, v in accepted.items() if fnmatch.fnmatch(fid, a))
                    rows.append((svc["name"], where, req.get("code", ""), "accepted", fid, f"{text} — принято: {why}"))
                    continue
                if level == "error":
                    failed += 1
                rows.append((svc["name"], where, req.get("code", ""), level, fid, text))

    if args.markdown:
        print("| сервис | запрос | код | уровень | проверка | детали |")
        print("|---|---|---|---|---|---|")
        for r in rows:
            print("| " + " | ".join(str(c).replace("|", "\\|") for c in r) + " |")
    else:
        for r in rows:
            print(f"[{r[3]:8}] {r[0]:12} {r[1]:60} {r[4]:28} {r[5]}  ({r[2]})")
    counts = {}
    for r in rows:
        counts[r[3]] = counts.get(r[3], 0) + 1
    print(f"\n{len(rows)} строк: " + ", ".join(f"{k}={v}" for k, v in sorted(counts.items())), file=sys.stderr)
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
