#!/usr/bin/env python3
"""Enable data on the first lab device and print overview."""

from __future__ import annotations

import json
import pathlib
import sys
import time
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:7575"
DEVICE = "ufi-34d12d26"


def load_auth() -> tuple[str, str]:
    user, password = "admin", ""
    pw = pathlib.Path("/opt/vohive/.admin-password")
    if pw.exists() and pw.stat().st_mode & 0o004:
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
    if not password and pw.exists():
        password = pw.read_text().strip()
    return user, password


def req(method: str, path: str, data=None, token: str | None = None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    body = None if data is None else json.dumps(data).encode()
    r = urllib.request.Request(BASE + path, data=body, headers=headers, method=method)
    try:
        with urllib.request.urlopen(r, timeout=60) as resp:
            return resp.status, json.loads(resp.read().decode() or "{}")
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read().decode() or "{}")


def main() -> int:
    user, password = load_auth()
    _, login = req("POST", "/api/auth/login", {"username": user, "password": password})
    token = (login.get("data") or {}).get("token")
    if not token:
        print("login failed")
        return 1

    st, ov = req("GET", f"/api/devices/{DEVICE}/overview", token=token)
    data = ov.get("data") if isinstance(ov, dict) else {}
    iccid = ""
    if isinstance(data, dict):
        iccid = str((data.get("modem") or {}).get("iccid") or data.get("iccid") or "")
    print("overview_iccid", iccid or "(none)")
    if iccid:
        st, pol = req(
            "PUT",
            f"/api/cards/{iccid}/policy",
            {"network_enabled": True},
            token,
        )
        print("policy_put", st, json.dumps(pol, ensure_ascii=False)[:500])

    st, patch = req(
        "PATCH",
        f"/api/devices/{DEVICE}/network",
        {"enabled": True},
        token,
    )
    print("network_patch", st)
    print(json.dumps(patch, ensure_ascii=False)[:800])

    for i in range(12):
        time.sleep(5)
        st, ov = req("GET", f"/api/devices/{DEVICE}/overview", token=token)
        data = ov.get("data") if isinstance(ov, dict) else ov
        if not isinstance(data, dict):
            print("overview", i, st, type(data))
            continue
        keys = (
            "lifecycle_phase",
            "healthy",
            "control_online",
            "data_connected",
            "radio_registered",
            "network_connected",
            "network_enabled",
            "public_ip",
            "interface",
        )
        snap = {k: data.get(k) for k in keys}
        modem = data.get("modem") or {}
        snap["operator"] = modem.get("operator")
        snap["signal_dbm"] = modem.get("signal_dbm")
        snap["reg_status"] = modem.get("reg_status")
        print(f"t={i*5+5}s", json.dumps(snap, ensure_ascii=False))
        if data.get("data_connected") or data.get("network_connected"):
            return 0
    return 0


if __name__ == "__main__":
    sys.exit(main())
