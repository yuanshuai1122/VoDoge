#!/usr/bin/env python3
"""Login, discover, and add one QMI stick by USB port / serial suffix."""

from __future__ import annotations

import argparse
import json
import pathlib
import sys
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:7575"


def load_pass() -> tuple[str, str]:
    user, password = "admin", ""
    pw = pathlib.Path("/opt/vohive/.admin-password")
    if pw.exists():
        password = pw.read_text().strip()
    cfg_path = pathlib.Path("/opt/vohive/config/config.yaml")
    try:
        cfg = cfg_path.read_text()
    except PermissionError:
        cfg = ""
    for line in cfg.splitlines():
        if line.strip().startswith("username:"):
            user = line.split(":", 1)[1].strip().strip("\"'")
        if line.strip().startswith("password:") and not password:
            password = line.split(":", 1)[1].strip().strip("\"'")
    if not password:
        raise SystemExit("no admin password found")
    return user, password


def req(method: str, path: str, data=None, token: str | None = None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    body = None if data is None else json.dumps(data).encode()
    r = urllib.request.Request(BASE + path, data=body, headers=headers, method=method)
    try:
        with urllib.request.urlopen(r, timeout=30) as resp:
            raw = resp.read().decode()
            return resp.status, json.loads(raw) if raw else {}
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        try:
            parsed = json.loads(raw)
        except Exception:
            parsed = {"raw": raw}
        return e.code, parsed


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--serial", required=True, help="ADB USB serial, e.g. 6726c019")
    p.add_argument("--usb-suffix", required=True, help="sysfs port suffix, e.g. 2-8")
    args = p.parse_args()

    user, password = load_pass()
    st, login = req("POST", "/api/auth/login", {"username": user, "password": password})
    token = ((login.get("data") or {}) if isinstance(login, dict) else {}).get("token")
    print("login", st, "token", "yes" if token else "no")
    if not token:
        print(json.dumps(login, ensure_ascii=False)[:800])
        return 1

    st, _ = req("POST", "/api/devices/actions/rescan", {}, token)
    print("rescan", st)
    st, disc = req("GET", "/api/devices/discovered?with_imei=1", token=token)
    payload = disc.get("data") if isinstance(disc, dict) else disc
    print("discovered", st)
    print(json.dumps(payload, ensure_ascii=False, indent=2)[:5000])

    items = payload if isinstance(payload, list) else []
    if isinstance(payload, dict):
        items = payload.get("devices") or payload.get("items") or []

    suffix = "/" + args.usb_suffix.lstrip("/")
    target = None
    for item in items:
        if not isinstance(item, dict):
            continue
        usb_path = str(item.get("usb_path") or "")
        if usb_path.endswith(suffix):
            target = item
            break
    if not target:
        print(f"no discovered device on {suffix}")
        return 2

    imei = str(target.get("imei") or "").strip()
    if not imei:
        print("discovered target has no IMEI")
        return 2

    device_id = f"ufi-{args.serial}"
    add = {
        "config": {
            "id": device_id,
            "name": f"UFI103S-{args.serial}",
            "modem_imei": imei,
            "device_backend": "qmi",
            "network_enabled": False,
            "sms_enabled": True,
        }
    }
    st, added = req("POST", "/api/devices", add, token)
    print("add", st)
    print(json.dumps(added, ensure_ascii=False, indent=2)[:2000])
    if st >= 400:
        return 3

    st, listing = req("GET", "/api/devices", token=token)
    print("list", st)
    print(json.dumps(listing.get("data") if isinstance(listing, dict) else listing, ensure_ascii=False, indent=2)[:4000])
    return 0


if __name__ == "__main__":
    sys.exit(main())
