#!/usr/bin/env python3
"""Read-only NVMe and PCIe health snapshot for Project Hub hosts."""

import argparse
import datetime as dt
import json
import os
import platform
import re
import shutil
import subprocess
import sys
from pathlib import Path


def read_text(path):
    try:
        return Path(path).read_text(encoding="utf-8", errors="replace").strip()
    except (OSError, ValueError):
        return None


def pci_link(root, bdf):
    base = Path(root) / "bus/pci/devices" / bdf
    if not base.is_dir():
        return None
    result = {
        "bdf": bdf,
        "current_speed": read_text(base / "current_link_speed"),
        "current_width": read_text(base / "current_link_width"),
        "max_speed": read_text(base / "max_link_speed"),
        "max_width": read_text(base / "max_link_width"),
    }
    pci_class = (read_text(base / "class") or "").lower()
    reports_downstream_link = pci_class.startswith("0x0604") or pci_class.startswith("0604")
    lspci = shutil.which("lspci")
    if lspci:
        try:
            completed = subprocess.run(
                [lspci, "-D", "-vv", "-s", bdf],
                check=False,
                capture_output=True,
                text=True,
                timeout=5,
            )
            output = completed.stdout + completed.stderr
            active = re.search(r"\bDLActive([+-])", output) if reports_downstream_link else None
            if active:
                result["link_active"] = active.group(1) == "+"
            elif reports_downstream_link and re.search(r"DownstreamComp:\s*Link Up\b", output):
                result["link_active"] = True
            elif reports_downstream_link and re.search(r"DownstreamComp:\s*Link Down\b", output):
                result["link_active"] = False
            else:
                result["link_active"] = None
            result["link_active_source"] = "root_port" if reports_downstream_link else "reported_by_upstream_root_port"
            link_status = re.search(r"^\s*LnkSta:\s*(.+)$", output, re.MULTILINE)
            downstream = re.search(r"^\s*DownstreamComp:\s*(.+)$", output, re.MULTILINE)
            result["lspci_link_status"] = link_status.group(1).strip() if link_status else None
            result["downstream_component"] = downstream.group(1).strip() if downstream else None
            if completed.returncode != 0:
                result["lspci_error"] = f"lspci exited with status {completed.returncode}"
        except (OSError, subprocess.TimeoutExpired) as exc:
            result["link_active"] = None
            result["lspci_error"] = str(exc)
    else:
        result["link_active"] = None
        result["lspci_error"] = "lspci is not installed"
        result["link_active_source"] = "root_port" if reports_downstream_link else "reported_by_upstream_root_port"
    return result


def smart_report(controller_name, device_path, model, firmware):
    smartctl = shutil.which("smartctl")
    nvme = shutil.which("nvme")
    if smartctl:
        command = [smartctl, "-j", "-a", device_path]
        tool = "smartctl"
    elif nvme:
        command = [nvme, "smart-log", device_path, "--output-format=json"]
        tool = "nvme-cli"
    else:
        return {
            "status": "unavailable",
            "reason": "neither smartctl nor nvme-cli is installed",
            "tool": None,
            "model": model,
            "firmware": firmware,
        }

    try:
        completed = subprocess.run(
            command,
            check=False,
            capture_output=True,
            text=True,
            timeout=15,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        return {
            "status": "unavailable",
            "reason": str(exc),
            "tool": tool,
            "model": model,
            "firmware": firmware,
        }

    try:
        data = json.loads(completed.stdout)
    except (json.JSONDecodeError, TypeError) as exc:
        return {
            "status": "unavailable",
            "reason": f"{tool} returned invalid JSON: {exc}",
            "tool": tool,
            "tool_exit_code": completed.returncode,
            "model": model,
            "firmware": firmware,
        }

    log = data.get("nvme_smart_health_information_log", data)
    smart_status = data.get("smart_status", {}).get("passed")
    if smart_status is True:
        status = "passed"
    elif smart_status is False:
        status = "failed"
    elif completed.returncode != 0:
        status = "warning"
    else:
        status = "available"

    temperature = log.get("temperature")
    if temperature is None:
        temperature = data.get("temperature", {}).get("current")
    media_errors = log.get("media_errors")
    if media_errors is None:
        media_errors = log.get("media_and_data_integrity_errors")
    model_from_smart = data.get("device", {}).get("model_name")
    report = {
        "status": status,
        "tool": tool,
        "tool_exit_code": completed.returncode,
        "model": model or model_from_smart,
        "firmware": firmware or data.get("firmware_version"),
        "temperature_c": temperature,
        "critical_warning": log.get("critical_warning"),
        "media_errors": media_errors,
        "error_log_entries": log.get("num_err_log_entries"),
    }
    if completed.stderr.strip():
        report["tool_stderr"] = completed.stderr.strip()
    if smart_status is None:
        report["device_smart_pass"] = None
    else:
        report["device_smart_pass"] = smart_status
    return report


def collect_report(sysfs_root="/sys"):
    root = Path(sysfs_root)
    controllers = []
    class_root = root / "class/nvme"
    if class_root.is_dir():
        for path in sorted(class_root.glob("nvme[0-9]*")):
            if not path.is_dir():
                continue
            name = path.name
            device = path / "device"
            resolved = device.resolve(strict=False)
            bdf = resolved.name if re.fullmatch(r"[0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.[0-7]", resolved.name) else None
            model = read_text(path / "model")
            firmware = read_text(path / "firmware_rev")
            item = {
                "controller": name,
                "device": f"/dev/{name}",
                "model": model,
                "firmware": firmware,
                "smart": smart_report(name, f"/dev/{name}", model, firmware),
                "pcie": pci_link(root, bdf) if bdf else None,
            }
            if not bdf:
                item["pcie_error"] = "NVMe controller is not linked to a PCI device in sysfs"
            controllers.append(item)

    root_ports = []
    pci_root = root / "bus/pci/devices"
    if pci_root.is_dir():
        for path in sorted(pci_root.iterdir()):
            pci_class = read_text(path / "class")
            if pci_class and pci_class.lower().startswith("0x0604"):
                link = pci_link(root, path.name)
                if link is not None:
                    root_ports.append(link)

    return {
        "timestamp_utc": dt.datetime.now(dt.timezone.utc).isoformat(timespec="seconds"),
        "kernel_release": platform.release(),
        "nvme_controllers": controllers,
        "pcie_root_ports": root_ports,
    }


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--sysfs-root", default="/sys", help=argparse.SUPPRESS)
    args = parser.parse_args(argv)
    json.dump(collect_report(args.sysfs_root), sys.stdout, separators=(",", ":"), sort_keys=True)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
