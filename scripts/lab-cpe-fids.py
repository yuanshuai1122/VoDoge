#!/usr/bin/env python3
import json
import re
import urllib.request

s = open("/tmp/cpe-app.js", encoding="utf-8", errors="ignore").read()
fids = sorted(set(re.findall(r'fid["\']?\s*[:=]\s*["\']([A-Za-z0-9_]+)["\']', s))
)
print("fids:")
for f in fids:
    print(f)
print("count", len(fids))
print("--- post snippets ---")
for m in re.finditer(r".{20}api/json.{90}", s):
    print(m.group(0))
    print("----")
