#!/usr/bin/env python3
import json
import re
import urllib.request

s = open("/tmp/cpe-app.js", encoding="utf-8", errors="ignore").read()
# field names used with this.fields.xxx
fields = sorted(set(re.findall(r"fields\.([A-Za-z0-9_]+)", s)))
print("field refs:")
for f in fields:
    print(f)

url = "http://192.168.100.1/api/json"


def post(payload):
    data = json.dumps(payload).encode()
    req = urllib.request.Request(url, data=data, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=8) as resp:
        return json.loads(resp.read().decode())


print("=== init ===")
print(json.dumps(post({"fid": "init"}), ensure_ascii=False)[:2000])
print("=== queryFields empty ===")
print(json.dumps(post({"fid": "queryFields", "fields": {}}), ensure_ascii=False)[:3000])
print("=== queryFields status-ish ===")
want = {
    k: ""
    for k in [
        "signal",
        "signalbar",
        "networkType",
        "netWorkMode",
        "operator",
        "simCardCurrent",
        "simState",
        "imsi",
        "iccid",
        "imei",
        "wanIp",
        "wan_ip",
        "pppStatus",
        "connectStatus",
        "roam",
        "usbMode",
    ]
}
print(json.dumps(post({"fid": "queryFields", "fields": want}), ensure_ascii=False)[:3000])
