#!/usr/bin/env python3
import re
import urllib.request

base = "http://192.168.100.1"
app = urllib.request.urlopen(base + "/static/js/app.c1f3e151a5db6a29a714.js", timeout=10).read().decode("utf-8", "ignore")
paths = sorted(set(re.findall(r"[\"'](/[A-Za-z0-9_./?-]{3,80})[\"']", app)))
print("paths:")
for p in paths[:100]:
    print(p)
print("--- keywords ---")
for k in sorted(set(re.findall(
    r"[\"']([A-Za-z0-9_]*(?:ppp|sim|wan|signal|operator|network_type|roam|imsi|iccid|connect|cgi)[A-Za-z0-9_]*)[\"']",
    app,
    re.I,
))):
    print(k)
