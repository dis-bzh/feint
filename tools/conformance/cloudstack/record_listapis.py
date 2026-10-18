#!/usr/bin/env python3
"""Keep, for the commands this pack serves, what a real listApis says about them.

    record_listapis.py listapis.json out.json listZones deployVirtualMachine ...

listapis.json is the raw answer of `listApis` from the official simulator (log in
as admin, call listApis with the session key; oracle.py shows the login). For each
named command the output keeps whether it is asynchronous and its parameters with
their type and whether they are required: the part of the API a client builds a
request from, and the part TestTheServedCommandsDeclareWhatTheServerDeclares holds
the pack to.
"""
import json
import sys

if len(sys.argv) < 4:
    sys.exit(__doc__)
source, out, names = sys.argv[1], sys.argv[2], sys.argv[3:]
apis = {a["name"]: a for a in json.load(open(source))["listapisresponse"]["api"]}
doc = {
    "source": "apache/cloudstack-simulator, CloudStack 4.23.0.0, listApis as admin",
    "image": "sha256:59db60234cd0ae7be289d79862de5818fbec4ac618d74b3af448efadbed77c95",
    "recorded": "2026-10-18",
    "total_commands": len(apis),
    "commands": {
        n: {
            "isasync": apis[n]["isasync"],
            "params": {p["name"]: {"type": p["type"], "required": p["required"]} for p in apis[n]["params"]},
        }
        for n in sorted(names)
    },
}
with open(out, "w") as f:
    json.dump(doc, f, indent=1)
print(len(doc["commands"]), "commands kept of", len(apis))
