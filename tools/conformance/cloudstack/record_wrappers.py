#!/usr/bin/env python3
"""Say, for every command a recording answered, which of three shapes its success has.

    record_wrappers.py real.json listapis.json tools/contract/cloudstack-wrappers.json

`listApis` describes the fields of what a command acts on and says nothing of how
the wire wraps them (tools/contract/extract-cloudstack.py explains). This reads a
recording of the official simulator (oracle.py real) and writes the three facts
the wire adds, for the commands the recording exercised:

  object  a plain answer: the fields under "<command>response"
  list    a count and the items under a key: the key is recorded
  async   a job: its jobid, and whatever else the acknowledgement carries (`id`
          for the commands that create)

A command is recorded only on a decisive answer. An empty list says nothing of its
key, so a command whose every recorded answer was empty is reported and the run
fails: a wrapper nobody saw is the invented format this is here to prevent.
"""
import json
import sys

if len(sys.argv) != 4:
    sys.exit(__doc__)
recording = json.load(open(sys.argv[1]))
apis = {a["name"]: a for a in json.load(open(sys.argv[2]))["listapisresponse"]["api"]}

commands = {}
undecided = set()
for step in recording:
    cmd, body = step["command"], step["body"]
    if cmd not in apis or step["status"] != 200 or not isinstance(body, dict) or len(body) != 1:
        continue
    key, inner = next(iter(body.items()))
    if key != cmd.lower() + "response" or not isinstance(inner, dict):
        continue

    if apis[cmd]["isasync"]:
        if "jobid" not in inner:
            sys.exit(f"{cmd}: asynchronous upstream and answered with no jobid: {inner}")
        entry = commands.setdefault(cmd, {"kind": "async", "ack": []})
        entry["ack"] = sorted(set(entry["ack"]) | (set(inner) - {"jobid"}))
    elif "count" in inner or inner == {} and cmd.startswith("list"):
        items = [k for k, v in inner.items() if k != "count" and isinstance(v, list)]
        if not items:
            undecided.add(cmd)  # an empty list names no key
            continue
        if len(items) != 1:
            sys.exit(f"{cmd}: a list with several item keys {items}")
        previous = commands.get(cmd)
        if previous and previous != {"kind": "list", "key": items[0]}:
            sys.exit(f"{cmd}: answered two different shapes: {previous} and {items[0]}")
        commands[cmd] = {"kind": "list", "key": items[0]}
    else:
        commands.setdefault(cmd, {"kind": "object"})

lost = sorted(undecided - set(commands))
if lost:
    sys.exit(f"recorded only empty lists, so their item key is unknown: {', '.join(lost)}")

doc = {
    "source": "apache/cloudstack-simulator, CloudStack 4.23.0.0, zone deployed by setup/dev/basic.cfg",
    "image": "sha256:59db60234cd0ae7be289d79862de5818fbec4ac618d74b3af448efadbed77c95",
    "recorded": "2026-10-18",
    "how": "tools/conformance/cloudstack/oracle.py real, then record_wrappers.py",
    "commands": dict(sorted(commands.items())),
}
with open(sys.argv[3], "w") as f:
    json.dump(doc, f, indent=1)
    f.write("\n")
for name, entry in doc["commands"].items():
    print(f"{name:26} {entry}")
