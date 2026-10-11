import json
import os
import shlex
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("nvme-health-check.py")


class NVMeHealthCheckTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.sysfs = self.root / "sys"
        self.bin = self.root / "bin"
        self.bin.mkdir()

    def tearDown(self):
        self.temp.cleanup()

    def add_pci_device(self, bdf, pci_class, *, current_speed, current_width, max_speed, max_width):
        device = self.sysfs / "bus/pci/devices" / bdf
        device.mkdir(parents=True)
        (device / "class").write_text(pci_class + "\n", encoding="ascii")
        for name, value in (
            ("current_link_speed", current_speed),
            ("current_link_width", current_width),
            ("max_link_speed", max_speed),
            ("max_link_width", max_width),
        ):
            (device / name).write_text(value + "\n", encoding="ascii")
        return device

    def add_controller(self, endpoint):
        controller = self.sysfs / "class/nvme/nvme0"
        controller.mkdir(parents=True)
        (controller / "model").write_text("KIOXIA TEST NVMe\n", encoding="ascii")
        (controller / "firmware_rev").write_text("TEST0001\n", encoding="ascii")
        (controller / "device").symlink_to(endpoint)

    def add_command(self, name, body):
        command = self.bin / name
        command.write_text("#!/bin/sh\n" + body, encoding="utf-8")
        command.chmod(0o755)
        return command

    def run_report(self, *, path=None):
        env = os.environ.copy()
        env["PATH"] = str(path or self.bin)
        return subprocess.run(
            [sys.executable, str(SCRIPT), "--sysfs-root", str(self.sysfs)],
            text=True,
            capture_output=True,
            env=env,
            check=False,
        )

    def test_reports_smart_health_and_active_gen3_links_without_serial(self):
        root_port = self.add_pci_device(
            "0000:00:00.0", "0x060400", current_speed="8.0 GT/s PCIe",
            current_width="1", max_speed="8.0 GT/s PCIe", max_width="1",
        )
        endpoint = self.add_pci_device(
            "0000:01:00.0", "0x010802", current_speed="8.0 GT/s PCIe",
            current_width="1", max_speed="8.0 GT/s PCIe", max_width="1",
        )
        self.add_controller(endpoint)
        smart = {
            "device": {"model_name": "KIOXIA TEST NVMe", "serial_number": "DO-NOT-EMIT"},
            "smart_status": {"passed": True},
            "nvme_smart_health_information_log": {
                "critical_warning": 0,
                "temperature": 57,
                "media_errors": 0,
                "num_err_log_entries": 1,
            },
        }
        self.add_command(
            "smartctl",
            "printf '%s\\n' " + shlex.quote(json.dumps(smart)) + "\nexit 0\n",
        )
        self.add_command(
            "lspci",
            "case \"$*\" in\n"
            "  *0000:00:00.0*) printf '%s\\n' 'LnkSta: Speed 8GT/s, Width x1' 'DLActive+' 'DownstreamComp: Link Up - Present' ;;\n"
            "  *0000:01:00.0*) printf '%s\\n' 'LnkSta: Speed 8GT/s, Width x1, DLActive-' ;;\n"
            "esac\n",
        )

        result = self.run_report()

        self.assertEqual(result.returncode, 0, result.stderr)
        report = json.loads(result.stdout)
        self.assertEqual(report["nvme_controllers"][0]["smart"]["temperature_c"], 57)
        self.assertEqual(report["nvme_controllers"][0]["smart"]["media_errors"], 0)
        self.assertEqual(report["nvme_controllers"][0]["smart"]["error_log_entries"], 1)
        self.assertEqual(report["nvme_controllers"][0]["pcie"]["bdf"], "0000:01:00.0")
        self.assertIsNone(report["nvme_controllers"][0]["pcie"]["link_active"])
        self.assertTrue(report["pcie_root_ports"][0]["link_active"])
        self.assertEqual(report["pcie_root_ports"][0]["current_speed"], "8.0 GT/s PCIe")
        self.assertNotIn("DO-NOT-EMIT", result.stdout)

    def test_reports_inactive_root_port_when_nvme_is_missing(self):
        self.add_pci_device(
            "0000:00:00.0", "0x060400", current_speed="2.5 GT/s PCIe",
            current_width="1", max_speed="8.0 GT/s PCIe", max_width="1",
        )
        self.add_command(
            "lspci",
            "printf '%s\\n' 'LnkSta: Speed 2.5GT/s, Width x1, DLActive-' 'DownstreamComp: Link Down - Present'\n",
        )

        result = self.run_report()

        self.assertEqual(result.returncode, 0, result.stderr)
        report = json.loads(result.stdout)
        self.assertEqual(report["nvme_controllers"], [])
        self.assertFalse(report["pcie_root_ports"][0]["link_active"])
        self.assertEqual(report["pcie_root_ports"][0]["current_speed"], "2.5 GT/s PCIe")

    def test_marks_smart_unavailable_if_no_smart_tool_exists(self):
        endpoint = self.add_pci_device(
            "0000:01:00.0", "0x010802", current_speed="8.0 GT/s PCIe",
            current_width="1", max_speed="8.0 GT/s PCIe", max_width="1",
        )
        self.add_controller(endpoint)

        result = self.run_report(path=self.root / "empty-path")

        self.assertEqual(result.returncode, 0, result.stderr)
        report = json.loads(result.stdout)
        self.assertEqual(report["nvme_controllers"][0]["smart"]["status"], "unavailable")
        self.assertIn("smartctl", report["nvme_controllers"][0]["smart"]["reason"])

    def test_malformed_smart_json_is_reported_without_aborting(self):
        endpoint = self.add_pci_device(
            "0000:01:00.0", "0x010802", current_speed="8.0 GT/s PCIe",
            current_width="1", max_speed="8.0 GT/s PCIe", max_width="1",
        )
        self.add_controller(endpoint)
        self.add_command("smartctl", "printf '%s\\n' 'not-json'\nexit 0\n")

        result = self.run_report()

        self.assertEqual(result.returncode, 0, result.stderr)
        report = json.loads(result.stdout)
        self.assertEqual(report["nvme_controllers"][0]["smart"]["status"], "unavailable")
        self.assertIn("JSON", report["nvme_controllers"][0]["smart"]["reason"])


if __name__ == "__main__":
    unittest.main()
