#!/usr/bin/env python3
"""One-shot: login, discover, add the first QMI stick by IMEI."""

from __future__ import annotations

import json
import pathlib
import sys
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:7575"


def load_pass() -> tuple[str, str]:
    cfg = pathlib.Path("/opt/vohive/config/config.yaml").read_text()
    user = "admin"
    password = ""
    for line in cfg.splitlines():
        if line.strip().startswith("username:"):
            user = line.split(":", 1)[1].strip().strip("\"'")
        if line.strip().startswith("password:"):
            password = line.split(":", 1)[1].strip().strip("\"'")
    if not password:
        p = pathlib.Path("/opt/vohive/.admin-password")
        if p.exists():
            password = p.read_text().strip()
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
    user, password = load_pass()
    st, login = req("POST", "/api/auth/login", {"username": user, "password": password})
    data = login.get("data") if isinstance(login, dict) else None
    token = data.get("token") if isinstance(data, dict) else None
    print("login", st, "token", "yes" if token else "no")
    if not token:
        print(json.dumps(login, ensure_ascii=False)[:800])
        return 1

    st, rescan = req("POST", "/api/devices/actions/rescan", {}, token)
    print("rescan", st)

    st, disc = req("GET", "/api/devices/discovered?with_imei=1", token=token)
    print("discovered", st)
    payload = disc.get("data") if isinstance(disc, dict) else disc
    print(json.dumps(payload, ensure_ascii=False, indent=2)[:5000])

    items = payload if isinstance(payload, list) else []
    if isinstance(payload, dict):
        items = payload.get("devices") or payload.get("items") or []

    target = None
    for item in items:
        if not isinstance(item, dict):
            continue
        imei = str(item.get("imei") or item.get("modem_imei") or "")
        backend = str(
            item.get("mode") or item.get("backend") or item.get("device_backend") or ""
        ).lower()
        usb_path = str(item.get("usb_path") or "")
        if backend == "qmi" or usb_path.endswith("/2-4"):
            target = item
            if usb_path.endswith("/2-4"):
                break
    if not target:
        print("no QMI target in discovery")
        return 2

    imei = str(target.get("imei") or target.get("modem_imei") or "").strip()
    if not imei:
        print("discovered target has no IMEI")
        return 2
    add = {
        "config": {
            "id": "ufi-34d12d26",
            "name": "UFI103S-34d12d26",
            "modem_imei": imei,
            "device_backend": "qmi",
            "network_enabled": True,
            "sms_enabled": True,
        }
    }
    st, added = req("POST", "/api/devices", add, token)
    print("add", st)
    print(json.dumps(added, ensure_ascii=False, indent=2)[:2000])

    st, listing = req("GET", "/api/devices", token=token)
    print("list", st)
    print(json.dumps(listing.get("data") if isinstance(listing, dict) else listing, ensure_ascii=False, indent=2)[:2000])
    return 0 if st < 400 else 3


if __name__ == "__main__":
    sys.exit(main())
