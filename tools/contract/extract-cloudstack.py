#!/usr/bin/env python3
"""Turn CloudStack's `listApis` answer into the contract the emulator checks against.

    extract-cloudstack.py contracts/cloudstack.json \\
        --listapis .upstream/cloudstack-listapis.json \\
        --wrappers tools/contract/cloudstack-wrappers.json \\
        --error-shape tools/contract/cloudstack-error.json \\
        --version 4.23.0.0 --source "..."

Why this is not extract-openapi.py
----------------------------------
CloudStack publishes no OpenAPI document. What it does publish is `listApis`: a
management server describes, for each of its commands, every parameter (name,
type, whether it is required) and every field of the object the command answers,
nested to four levels. That is the same kind of fact an OpenAPI document holds,
read from the server itself, and it is the only description that cannot be older
than the server it came from.

It is not a description of the wire, and that is the whole difficulty. Three
things the wire does are written nowhere in it:

  * every answer is wrapped under "<command>response", lower-cased;
  * a command that changes something answers a job (`jobid`, and `id` for the
    ones that create), and the fields `listApis` lists for it describe what the
    job will eventually settle on, not what the command answers;
  * a list answers `count` and its items under a key that is the item's own name
    ("zone", "virtualmachine"), which `listApis` does not give.

The first is true of every success the server has ever been recorded giving. The
other two differ per command, so they are not guessed: --wrappers names, for the
commands whose answers have been recorded, which of the three shapes each is and
what its list key and its acknowledgement carry, and tools/conformance/cloudstack/
record_wrappers.py writes that file from a recording of the official simulator.

So the artefact is honest in two tiers. Every command gets what the server
declared of it: its name, the verb it prefers, its parameters, which of them are
required, and whether it is asynchronous. A command also gets the schema of its
answer when, and only when, an answer of that command has been recorded. Serving
one more command is therefore: record it, add it to the wrappers, regenerate. A
response schema nobody recorded would be exactly the invented format this
project refuses to ship.

Closed schemas
--------------
`listApis` does not say whether the fields it lists are all the server answers,
so the policy is "assumed", as for Exoscale, and it is held by a measurement
rather than by hope: every field the simulator answered for the seven commands
recorded on 2026-10-18 is declared by its own listApis, and the one place the two
seemed to disagree (`downloaddetails`) is a list the server declares without
describing its items, which this extractor records as an array of undescribed
items instead of closing it. TestTheRecordingValidatesAgainstTheContract replays
the recording against the artefact on every run.

Types
-----
Only what is known to be a JSON primitive is typed: string, uuid, date and url are
strings; boolean; integer, int, long and short are integers; float and double are
numbers; list and set are arrays (of the nested object when `listApis` describes
it); map is an object. Everything else, a Java enumeration or a response class
whose items are not described, is left untyped, because a wrong type makes the
validator accuse a correct answer and an absent one accuses nothing. The count of
what was left untyped is printed so it stays a number somebody has seen.

The error shape
---------------
A refusal is the same envelope under "<command>response" (or "errorresponse" for a
command the server does not know) carrying `uuidList`, `errorcode`, `cserrorcode`
and `errortext`. `listApis` does not describe it; --error-shape supplies it,
transcribed from ExceptionResponse.java, and the file carries the citation. Rule 4:
the shape comes from the source, never from a guess.
"""
import argparse
import json
import sys

# What a listApis type is, as a JSON value. Anything absent is left untyped.
SCALAR = {
    "string": "string",
    "uuid": "string",
    "date": "string",
    "url": "string",
    "boolean": "boolean",
    "integer": "integer",
    "int": "integer",
    "long": "integer",
    "short": "integer",
    "float": "number",
    "double": "number",
}
UNTYPED = {}


class Builder:
    def __init__(self):
        self.schemas = {}
        self.untyped = {}

    def prop(self, field, schema_name):
        """The Property of one listApis field, creating the nested schema it needs."""
        kind = field.get("type", "")
        nested = [f for f in (field.get("response") or []) if f]
        if kind in SCALAR:
            return {"type": SCALAR[kind]}
        if kind in ("list", "set"):
            if nested:
                self.object(schema_name, nested)
                return {"type": "array", "items": {"ref": schema_name}}
            # Declared a list, described nothing of its items.
            return {"type": "array"}
        if kind == "map":
            return {"type": "object"}
        if kind.endswith("[]"):
            base = SCALAR.get(kind[:-2])
            return {"type": "array", "items": {"type": base}} if base else {"type": "array"}
        if nested:
            self.object(schema_name, nested)
            return {"ref": schema_name}
        if kind.endswith("response") or kind == "responseobject":
            return {"type": "object"}
        self.untyped[kind] = self.untyped.get(kind, 0) + 1
        return dict(UNTYPED)

    def object(self, name, fields, required=None, extra=None):
        """A closed schema from a listApis field list; `extra` adds fields it does not carry."""
        props = {}
        for f in fields:
            if not f or "name" not in f:
                continue  # listApis pads its lists with empty objects
            props[f["name"]] = self.prop(f, f"{name}.{f['name']}")
        props.update(extra or {})
        schema = {"closed": True, "properties": dict(sorted(props.items()))}
        if required:
            schema["required"] = sorted(required)
        self.schemas[name] = schema
        return name


def param_property(param):
    kind = param["type"]
    if kind in SCALAR:
        return {"type": SCALAR[kind]}
    if kind == "list":
        return {"type": "array", "items": {"type": "string"}}
    if kind in ("map", "object"):
        return {"type": "object"}
    return dict(UNTYPED)


def pascal(command):
    return command[0].upper() + command[1:]


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("out")
    ap.add_argument("--listapis", required=True, help="the raw answer of listApis from a management server")
    ap.add_argument("--wrappers", required=True, help="the recorded wrapper facts of the commands the pack serves")
    ap.add_argument("--error-shape", required=True, help="the error body, with its citation")
    ap.add_argument("--version", required=True, help="the management server's version, e.g. 4.23.0.0")
    ap.add_argument("--source", required=True)
    args = ap.parse_args()

    apis = json.load(open(args.listapis))["listapisresponse"]["api"]
    wrappers = json.load(open(args.wrappers))
    error = json.load(open(args.error_shape))

    by_name = {a["name"]: a for a in apis}
    unknown = sorted(set(wrappers["commands"]) - set(by_name))
    if unknown:
        sys.exit(f"wrappers name commands the server does not list: {', '.join(unknown)}")

    b = Builder()
    operations = {}
    for a in sorted(apis, key=lambda x: x["name"]):
        name = a["name"]
        query, required = {}, []
        for p in sorted(a["params"], key=lambda x: x["name"]):
            query[p["name"]] = param_property(p)
            if p["required"]:
                required.append(p["name"])
        op = {
            "path": "?command=" + name,
            "method": a["httprequesttype"],
            "request": "",
            "response": "",
        }
        if query:
            op["query"] = query
        if required:
            op["queryRequired"] = required
        if a["isasync"]:
            op["async"] = True

        seen = wrappers["commands"].get(name)
        if seen:
            op["response"] = response_schema(b, a, seen)
        operations[name] = op

    for name, schema in error["schemas"].items():
        b.schemas[name] = schema

    doc = {
        "provider": "cloudstack",
        "source": args.source,
        "apiVersion": args.version,
        "pathPrefix": "/client/api",
        "closedPolicy": "assumed",
        "errorSchema": error["errorSchema"],
        "operations": operations,
        "schemas": dict(sorted(b.schemas.items())),
    }
    with open(args.out, "w") as f:
        json.dump(doc, f, indent=1)
        f.write("\n")

    served = sum(1 for o in operations.values() if o["response"])
    sys.stderr.write(
        f"{len(operations)} commands, {sum(len(o.get('query', {})) for o in operations.values())} parameters, "
        f"{sum(1 for o in operations.values() if o.get('async'))} asynchronous, "
        f"{served} with a recorded answer, {len(b.schemas)} schemas\n"
    )
    if b.untyped:
        sys.stderr.write("left untyped (not a JSON primitive): " + ", ".join(f"{k}={v}" for k, v in sorted(b.untyped.items())) + "\n")


def response_schema(b, api, seen):
    """The schemas of one recorded command's answer; returns the name of the outermost."""
    name = pascal(api["name"])
    lower = api["name"].lower() + "response"
    kind = seen["kind"]
    body = name + "Body"

    if kind == "object":
        b.object(body, api["response"])
    elif kind == "list":
        item = name + "Item"
        b.object(item, api["response"])
        b.schemas[body] = {
            "closed": True,
            "properties": {
                "count": {"type": "integer"},
                seen["key"]: {"type": "array", "items": {"ref": item}},
            },
        }
    elif kind == "async":
        ack = {k: {"type": "string"} for k in ["jobid"] + seen.get("ack", [])}
        b.schemas[body] = {"closed": True, "required": sorted(ack), "properties": dict(sorted(ack.items()))}
    else:
        sys.exit(f"{api['name']}: wrapper kind {kind!r} is not one of object, list, async")

    envelope = name + "Response"
    b.schemas[envelope] = {"closed": True, "properties": {lower: {"ref": body}}}
    return envelope


if __name__ == "__main__":
    main()
