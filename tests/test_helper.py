"""Public API glue tests only. Never connects to hardware."""

from contextlib import contextmanager
import importlib.util
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import MagicMock, patch

from yubikit.management import CAPABILITY
from yubikit.oath import OATH_TYPE

spec = importlib.util.spec_from_file_location(
    "bridge", Path(__file__).resolve().parents[1] / "internal/backend/helper.py"
)
bridge = importlib.util.module_from_spec(spec)
spec.loader.exec_module(bridge)

# Public RFC 6238 test vector, not a real account seed.
RFC_VECTOR = (
    "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"  # gitleaks:allow -- public RFC test data
)


class HelperTests(unittest.TestCase):
    def setUp(self):
        self.info = SimpleNamespace(
            fips_capable=CAPABILITY(0), fips_approved=CAPABILITY(0)
        )
        self.writes = []
        self.session = SimpleNamespace(
            list_credentials=list,
            put_credential=self.put,
            calculate_code=lambda *_args, **_kwargs: SimpleNamespace(
                value="not-a-valid-code"
            ),
        )

    def put(self, data, touch_required):
        self.writes.append((data, touch_required))
        return data

    @contextmanager
    def connection(self, _request):
        yield self.session, self.info

    def request(self, op="oath-preview"):
        return {
            "op": op,
            "serial": "11111111",
            "confirm": "11111111",
            "name": "Demo",
            "seed": RFC_VECTOR,
        }

    def test_preview_never_writes(self):
        with patch.object(bridge, "oath", self.connection):
            result = bridge.mutate(self.request())
        self.assertIn("no write", result["message"])
        self.assertEqual(self.writes, [])

    def test_duplicate_and_fips_preconditions_do_not_overwrite(self):
        request = self.request()
        data = bridge.validate_new_oath(self.session, self.info, request)
        self.session.list_credentials = lambda: [SimpleNamespace(id=data.get_id())]
        with self.assertRaises(bridge.Failure) as caught:
            bridge.validate_new_oath(self.session, self.info, request)
        self.assertEqual(caught.exception.code, "conflict")
        self.info.fips_capable = CAPABILITY.OATH
        with self.assertRaises(bridge.Failure) as caught:
            bridge.validate_new_oath(self.session, self.info, request)
        self.assertEqual(caught.exception.code, "fips_unready")
        self.assertEqual(self.writes, [])

    def test_hotp_migration_is_rejected(self):
        request = self.request()
        request["seed"] = f"otpauth://hotp/demo?secret={RFC_VECTOR}&counter=1"
        with self.assertRaises(bridge.Failure):
            bridge.validate_new_oath(self.session, self.info, request)
        self.assertEqual(self.writes, [])

    def test_confirmation_required_before_opening_hardware(self):
        request = self.request("oath-add")
        request["confirm"] = "22222222"
        with patch.object(bridge, "oath", side_effect=AssertionError("must not open")):
            with self.assertRaises(bridge.Failure) as caught:
                bridge.mutate(request)
        self.assertEqual(caught.exception.code, "confirm_required")

    def test_postwrite_verification_failure_is_not_rollback(self):
        with patch.object(bridge, "oath", self.connection):
            with self.assertRaises(bridge.Failure) as caught:
                bridge.mutate(self.request("oath-add"))
        self.assertEqual(len(self.writes), 1)
        self.assertTrue(self.writes[0][1])
        self.assertEqual(caught.exception.code, "outcome_unknown")

    def test_hotp_generation_requires_explicit_counter_consent(self):
        credential = SimpleNamespace(id=b"test", oath_type=OATH_TYPE.HOTP)
        self.session.list_credentials = lambda: [credential]
        self.session.calculate_code = lambda *_: self.fail(
            "counter advanced without consent"
        )
        with patch.object(bridge, "oath", self.connection):
            with self.assertRaises(bridge.Failure) as caught:
                bridge.get_code({"serial": "11111111", "id": b"test".hex()})
        self.assertEqual(caught.exception.code, "confirm_required")

    def test_discovery_associates_readable_key_with_exact_reader(self):
        name = "Yubico Demo CCID 0"
        reader = MagicMock()
        reader.__str__.return_value = name
        device = SimpleNamespace(fingerprint=name)
        info = SimpleNamespace(serial=11111111)
        entry = {"id": "yubikey:11111111", "kind": "yubikey"}
        with (
            patch("ykman.device.list_all_devices", return_value=[(device, info)]),
            patch.object(bridge, "source", return_value=entry),
            patch("smartcard.System.readers", return_value=[reader]),
        ):
            rows = bridge.sources()
        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0]["reader_ids"], ["reader:" + name])

    def test_ejected_reader_remains_visible_without_a_guessed_serial(self):
        reader = MagicMock()
        reader.__str__.return_value = "Yubico Demo CCID 0"
        with (
            patch("ykman.device.list_all_devices", return_value=[]),
            patch("smartcard.System.readers", return_value=[reader]),
        ):
            rows = bridge.sources()
        self.assertEqual(rows[0]["kind"], "reader")
        self.assertEqual(rows[0]["state"], "unavailable")
        self.assertNotIn("serial", rows[0])


if __name__ == "__main__":
    unittest.main()
