#!/usr/bin/env python3
"""Compare two oracle recordings: statuses, field sets and error sentences.

    compare.py real.json mine.json [accepted.json]

accepted.json lists the differences knowingly left, each with the reason: a step,
the kind of difference ("missing" or "extra") and the field paths it covers. It is
the register corpus/accepted.json is for the other packs, with the same two rules:
an entry without a reason is refused, and an entry that covers nothing is refused
too, because an exemption that outlived its cause reads like a considered one.

What it does not compare is values: identifiers, dates and addresses differ by
construction. A step that differs because the two servers were in different
states (a machine left over from an earlier run, a sequence number that keeps
growing) shows up too; read the difference before believing it.
"""
import json
import re
import sys

if len(sys.argv) not in (3, 4):
    sys.exit(__doc__)
real = {s["label"]: s for s in json.load(open(sys.argv[1]))}
mine = {s["label"]: s for s in json.load(open(sys.argv[2]))}
accepted = json.load(open(sys.argv[3])) if len(sys.argv) > 3 else []
for entry in accepted:
    if len(entry.get("reason", "").split()) < 8:
        sys.exit(f"accepted entry {entry.get('step')}/{entry.get('kind')} carries no reason a reader can weigh")
used = set()
OPAQUE = {"details"}


def kind(v):
    return "bool" if isinstance(v, bool) else "num" if isinstance(v, (int, float)) else "str" if isinstance(v, str) else "null" if v is None else type(v).__name__


def paths(v, p=""):
    out = {}
    if isinstance(v, dict):
        for k, x in v.items():
            q = f"{p}.{k}" if p else k
            out[q] = kind(x)
            if k not in OPAQUE:
                out.update(paths(x, q))
    elif isinstance(v, list):
        for x in v:
            out.update(paths(x, p + "[]"))
    return out


def error(body):
    x = next(iter(body.values())) if isinstance(body, dict) and len(body) == 1 else body
    if not isinstance(x, dict):
        return None
    r = x.get("jobresult") if isinstance(x.get("jobresult"), dict) else x
    if "errortext" not in r and "errortext" not in x:
        return None
    return (r.get("errorcode", x.get("errorcode")), x.get("cserrorcode"), r.get("errortext", x.get("errortext")))


uuid = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")
bad = 0
for label, r in real.items():
    m = mine.get(label)
    if m is None:
        print(f"-- {label}: only in the first recording")
        bad += 1
        continue
    notes = []
    if r["status"] != m["status"]:
        notes.append(f"STATUS {r['status']} / {m['status']}")
    pr, pm = paths(r["body"]), paths(m["body"])
    miss = sorted(k for k in pr if k not in pm)
    extra = sorted(k for k in pm if k not in pr)

    def unaccepted(paths, kind):
        left = []
        for path in paths:
            for i, entry in enumerate(accepted):
                if entry["step"] == label and entry["kind"] == kind and path in entry["paths"]:
                    used.add((i, path))
                    break
            else:
                left.append(path)
        return left

    miss, extra = unaccepted(miss, "missing"), unaccepted(extra, "extra")
    wrong = sorted(k for k in pr if k in pm and pr[k] != pm[k] and "null" not in (pr[k], pm[k]))
    if miss:
        notes.append("missing: " + ", ".join(miss[:8]) + (f" (+{len(miss) - 8})" if len(miss) > 8 else ""))
    if extra:
        notes.append("extra: " + ", ".join(extra[:8]) + (f" (+{len(extra) - 8})" if len(extra) > 8 else ""))
    if wrong:
        notes.append("type: " + ", ".join(f"{k} {pr[k]}/{pm[k]}" for k in wrong[:6]))
    er, em = error(r["body"]), error(m["body"])
    if er and em:
        # Sequence numbers are the server's database counter: they grow with every
        # run against the same server, so they are never a difference.
        norm = lambda t: re.sub(r"with id \d+", "with id N", re.sub(r"i-2-\d+-VM", "i-2-N-VM", uuid.sub("<uuid>", str(t))))
        if er[0] != em[0] or er[1] != em[1] or norm(er[2]) != norm(em[2]):
            notes.append(f"ERROR {er[0]}/{er[1]} {norm(er[2])!r:.90} / {em[0]}/{em[1]} {norm(em[2])!r:.90}")
    elif bool(er) != bool(em):
        notes.append(f"ERROR only in one: {er} / {em}")
    if notes:
        bad += 1
        print(f"-- {label}: " + " | ".join(notes))
stale = [(i, p) for i, entry in enumerate(accepted) for p in entry["paths"] if (i, p) not in used]
for i, path in stale:
    print(f"-- accepted entry {accepted[i]['step']}/{accepted[i]['kind']} covers nothing: {path}")
    bad += 1
print(f"{len(real)} steps compared, {len(used)} differences accepted, {bad} differ")
sys.exit(1 if bad else 0)
