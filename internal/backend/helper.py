# /// script
# requires-python = ">=3.11"
# dependencies = ["yubikey-manager==5.9.2", "pyotp==2.9.0"]
# ///
"""Private stdin/stdout bridge to public YubiKey Manager APIs. No protocol implementation."""

import base64
import contextlib
import hashlib
import json
import logging
import sys
import time

logging.disable(logging.CRITICAL)


class Failure(Exception):
    def __init__(self, code, message):
        self.code, self.message = code, message


def field(name, value, secret=False, copyable=True):
    return {
        "id": name,
        "label": name.replace("_", " ").title(),
        "value": str(value),
        "secret": secret,
        "copyable": copyable,
    }


def row(identifier, name, kind, fields=(), state="ready", subtitle=""):
    return {
        "id": identifier,
        "name": name,
        "kind": kind,
        "fields": list(fields),
        "state": state,
        "subtitle": subtitle,
    }


def require_auth(request):
    if not request.get("auth"):
        raise Failure("locked", "Unlock this application with its PIN/password first.")
    return request["auth"]


def select_device(serial, connection_type):
    from ykman.device import list_all_devices

    matches = [
        (d, i)
        for d, i in list_all_devices([connection_type])
        if str(i.serial) == serial
    ]
    if len(matches) != 1:
        raise Failure(
            "disconnected",
            "The exact device is unavailable. For CCID-only touch-eject, touch once and refresh.",
        )
    return matches[0]


def source(device, info):
    from yubikit.core import TRANSPORT

    supported = info.supported_capabilities.get(TRANSPORT.USB, 0)
    enabled = info.config.enabled_capabilities.get(TRANSPORT.USB, supported)
    name = "YubiKey" + (" FIPS" if info.is_fips else "")
    return {
        "id": f"yubikey:{info.serial}",
        "kind": "yubikey",
        "name": f"{name} · {info.serial}",
        "state": "ready",
        "summary": f"Firmware {info.version}",
        "fields": [
            field("serial", info.serial),
            field("firmware", info.version),
            field("form_factor", info.form_factor.name),
            field("usb_enabled", getattr(enabled, "name", str(enabled))),
            field("usb_supported", getattr(supported, "name", str(supported))),
            field("fips_capable", info.fips_capable.name),
            field("fips_approved", info.fips_approved.name),
            field("pin_complexity", info.pin_complexity),
        ],
    }


def sources():
    from ykman.device import list_all_devices

    devices = list_all_devices()
    result = [source(d, i) for d, i in devices if i.serial is not None]
    # An ejected CCID card still exposes its reader. Preserve that distinct state.
    with contextlib.suppress(Exception):
        from smartcard.System import readers

        # Match the SDK's PC/SC fingerprint to the actual reader name. Keep
        # this exact identity so a reader placeholder can become its key in UI.
        by_reader = {
            str(d.fingerprint): entry
            for (d, info), entry in zip(
                [(d, i) for d, i in devices if i.serial is not None], result
            )
        }
        for reader in readers():
            name = str(reader)
            if name in by_reader:
                by_reader[name]["reader_ids"] = ["reader:" + name]
            elif "yubi" in name.lower():
                result.append(
                    {
                        "id": "reader:" + name,
                        "kind": "reader",
                        "name": name,
                        "state": "unavailable",
                        "summary": "Reader visible; card cannot be opened. Touch-eject may require one touch.",
                    }
                )
    return result


@contextlib.contextmanager
def oath(request):
    from yubikit.core.smartcard import SmartCardConnection
    from yubikit.oath import OathSession

    device, info = select_device(request["serial"], SmartCardConnection)
    with device.open_connection(SmartCardConnection) as connection:
        session = OathSession(connection)
        if session.locked:
            session.validate(session.derive_key(require_auth(request)))
        yield session, info


@contextlib.contextmanager
def fido(request):
    from yubikit.core.fido import FidoConnection
    from fido2.ctap2 import Ctap2

    device, info = select_device(request["serial"], FidoConnection)
    with device.open_connection(FidoConnection) as connection:
        yield Ctap2(connection), info


def credential_manager(ctap, request):
    from fido2.ctap2.credman import CredentialManagement
    from fido2.ctap2.pin import ClientPin

    pin = ClientPin(ctap)
    token = pin.get_pin_token(
        require_auth(request), ClientPin.PERMISSION.CREDENTIAL_MGMT
    )
    return CredentialManagement(ctap, pin.protocol, token)


def fido_entries(manager):
    from fido2.ctap2.credman import CredentialManagement as CM
    from fido2.ctap import CtapError

    try:
        parties = manager.enumerate_rps()
    except CtapError as error:
        if error.code == CtapError.ERR.NO_CREDENTIALS:
            return []
        raise
    result = []
    for party in parties:
        rp = party[CM.RESULT.RP]
        for credential in manager.enumerate_creds(party[CM.RESULT.RP_ID_HASH]):
            descriptor = credential[CM.RESULT.CREDENTIAL_ID]
            identifier = descriptor["id"].hex()
            user = credential[CM.RESULT.USER]
            result.append(
                row(
                    identifier,
                    user.get("name")
                    or user.get("displayName")
                    or rp.get("id", "Credential"),
                    "fido",
                    [
                        field("credential_id", identifier),
                        field("rp_id", rp.get("id", "")),
                        field("username", user.get("name", "")),
                        field("display_name", user.get("displayName", "")),
                        field("user_id", user.get("id", b"").hex()),
                    ],
                    subtitle=rp.get("id", ""),
                )
            )
    return result


def fido_status(request):
    with fido(request) as (ctap, info):
        data = ctap.get_info()
        fields = [
            field("versions", ", ".join(data.versions)),
            field("pin_set", data.options.get("clientPin", "unknown")),
            field("always_uv", data.options.get("alwaysUv", "unknown")),
            field("minimum_pin_length", data.min_pin_length),
            field(
                "remaining_discoverable_credentials",
                data.remaining_disc_creds
                if data.remaining_disc_creds is not None
                else "unknown",
            ),
            field(
                "algorithms",
                ", ".join(str(a.get("alg")) for a in data.algorithms or []),
            ),
            field(
                "credential_scope",
                "Discoverable credentials only; not all historical website registrations",
                copyable=False,
            ),
        ]
        return row("fido-status", "FIDO2 status", "status", fields), source(None, info)


def items(request):
    category = request.get("category", "status")
    if category == "oath":
        with oath(request) as (session, _):
            return [
                row(
                    c.id.hex(),
                    f"{c.issuer + ':' if c.issuer else ''}{c.name}",
                    "oath",
                    [
                        field("credential_id", c.id.hex()),
                        field("issuer", c.issuer or ""),
                        field("username", c.name),
                        field("oath_type", c.oath_type.name),
                        field("period", c.period),
                        field("touch", c.touch_required),
                        field("otp", "", secret=True),
                    ],
                    subtitle=c.oath_type.name,
                )
                for c in session.list_credentials()
            ]
    if category == "fido":
        with fido(request) as (ctap, _):
            return fido_entries(credential_manager(ctap, request))
    from yubikit.core.smartcard import SmartCardConnection

    try:
        device, info = select_device(request["serial"], SmartCardConnection)
    except Failure:
        if category == "status":
            status, device_source = fido_status(request)
            return [row("device", "Device", "status", device_source["fields"]), status]
        raise
    with device.open_connection(SmartCardConnection) as connection:
        if category == "piv":
            from ykman.piv import list_certificates
            from yubikit.piv import PivSession, SLOT
            from cryptography.hazmat.primitives import hashes, serialization

            session = PivSession(connection)
            certificates = list_certificates(session)
            result = []
            for slot in SLOT:
                if slot == SLOT.ATTESTATION:
                    continue
                certificate = certificates.get(slot)
                fields = [
                    field("slot", f"{slot:02x}"),
                    field(
                        "private_key",
                        "Non-exportable; certificate alone does not prove a matching key",
                        copyable=False,
                    ),
                ]
                state = "unknown"
                if info.version >= (5, 3, 0):
                    with contextlib.suppress(Exception):
                        metadata = session.get_slot_metadata(slot)
                        fields.extend(
                            [
                                field("key_type", metadata.key_type.name),
                                field("pin_policy", metadata.pin_policy.name),
                                field("touch_policy", metadata.touch_policy.name),
                            ]
                        )
                        state = "key_present"
                if certificate:
                    fields.extend(
                        [
                            field("subject", certificate.subject.rfc4514_string()),
                            field("issuer", certificate.issuer.rfc4514_string()),
                            field(
                                "expires", certificate.not_valid_after_utc.isoformat()
                            ),
                            field(
                                "fingerprint_sha256",
                                certificate.fingerprint(hashes.SHA256()).hex(),
                            ),
                            field(
                                "certificate",
                                certificate.public_bytes(
                                    serialization.Encoding.PEM
                                ).decode(),
                            ),
                            field(
                                "public_key",
                                certificate.public_key()
                                .public_bytes(
                                    serialization.Encoding.PEM,
                                    serialization.PublicFormat.SubjectPublicKeyInfo,
                                )
                                .decode(),
                            ),
                        ]
                    )
                    from datetime import datetime, timezone

                    state = (
                        "expired_certificate"
                        if certificate.not_valid_after_utc < datetime.now(timezone.utc)
                        else "certificate_present"
                    )
                result.append(
                    row(
                        f"{slot:02x}",
                        f"Slot {slot:02X} · {slot.name}",
                        "piv",
                        fields,
                        state=state,
                    )
                )
            return result
        if category == "openpgp":
            from yubikit.openpgp import OpenPgpSession, KEY_REF
            from ykman.openpgp import get_openpgp_info

            session = OpenPgpSession(connection)
            data = session.get_application_related_data()
            result = [
                row(
                    "openpgp-status",
                    "OpenPGP status",
                    "status",
                    [
                        field(k, v, copyable=False)
                        for k, v in get_openpgp_info(session).items()
                        if not isinstance(v, dict)
                    ],
                )
            ]
            for reference, fingerprint in data.discretionary.fingerprints.items():
                if reference != KEY_REF.ATT and any(fingerprint):
                    result.append(
                        row(
                            str(reference.value),
                            reference.name,
                            "openpgp",
                            [field("fingerprint", fingerprint.hex())],
                        )
                    )
            return result
        if category != "status":
            raise Failure("invalid", "Unknown YubiKey category.")
        from yubikit.management import CAPABILITY
        from yubikit.core import TRANSPORT

        enabled = info.config.enabled_capabilities.get(
            TRANSPORT.USB, info.supported_capabilities.get(TRANSPORT.USB, 0)
        )
        result = [row("device", "Device", "status", source(device, info)["fields"])]
        for capability, app in (
            (CAPABILITY.OATH, "OATH"),
            (CAPABILITY.PIV, "PIV"),
            (CAPABILITY.OPENPGP, "OpenPGP"),
            (CAPABILITY.FIDO2, "FIDO2"),
        ):
            supported = capability in info.supported_capabilities.get(TRANSPORT.USB, 0)
            state = (
                "enabled"
                if capability in enabled
                else ("disabled" if supported else "not_supported")
            )
            fields = [field("state", state, copyable=False)]
            try:
                if state == "enabled" and capability == CAPABILITY.OATH:
                    from yubikit.oath import OathSession

                    session = OathSession(connection)
                    fields.append(field("locked", session.locked, copyable=False))
                    fields.append(
                        field(
                            "accounts",
                            "unknown (locked)"
                            if session.locked
                            else len(session.list_credentials()),
                            copyable=False,
                        )
                    )
                elif state == "enabled" and capability == CAPABILITY.PIV:
                    from yubikit.piv import PivSession
                    from ykman.piv import list_certificates

                    session = PivSession(connection)
                    fields.extend(
                        [
                            field(
                                "pin_attempts",
                                session.get_pin_attempts(),
                                copyable=False,
                            ),
                            field(
                                "certificates",
                                len(list_certificates(session)),
                                copyable=False,
                            ),
                        ]
                    )
            except Exception:
                state = "unavailable"
                fields.append(
                    field(
                        "read",
                        "Could not read application; no PIN attempted",
                        copyable=False,
                    )
                )
            result.append(row(app.lower(), app, "status", fields, state=state))
        if CAPABILITY.FIDO2 in enabled:
            try:
                status, _ = fido_status(request)
                result.append(status)
            except Exception:
                result.append(
                    row("fido-status", "FIDO2 status", "status", state="unavailable")
                )
        return result


def get_code(request):
    from yubikit.oath import OATH_TYPE

    with oath(request) as (session, _):
        matches = [c for c in session.list_credentials() if c.id.hex() == request["id"]]
        if len(matches) != 1:
            raise Failure("not_found", "Exact OATH credential no longer exists.")
        credential = matches[0]
        if (
            credential.oath_type == OATH_TYPE.HOTP
            and request.get("confirm") != request["serial"]
        ):
            raise Failure(
                "confirm_required",
                "HOTP advances its counter; confirm the target serial first.",
            )
        return {"value": session.calculate_code(credential).value}


def validate_new_oath(session, info, request):
    from yubikit.management import CAPABILITY
    from yubikit.oath import CredentialData, OATH_TYPE

    if (
        CAPABILITY.OATH in info.fips_capable
        and CAPABILITY.OATH not in info.fips_approved
    ):
        raise Failure(
            "fips_unready",
            "FIPS OATH must be in approved mode before adding accounts. Configure it with the official tools.",
        )
    seed = request.get("seed", "").strip()
    if seed.lower().startswith("otpauth://"):
        data = CredentialData.parse_uri(seed)
    else:
        from urllib.parse import quote, urlencode

        data = CredentialData.parse_uri(
            "otpauth://totp/"
            + quote(request.get("name", ""), safe="")
            + "?"
            + urlencode({"secret": seed})
        )
    if request.get("name"):
        # An explicit label is the target account name; preserve URI issuer.
        data.name = request["name"]
    if (
        data.oath_type != OATH_TYPE.TOTP
        or not data.name
        or not data.secret
        or data.period <= 0
        or data.digits not in (6, 7, 8)
    ):
        raise Failure(
            "invalid",
            "Only standard TOTP is accepted; HOTP/Steam migration is not supported.",
        )
    if len(data.get_id()) > 64:
        raise Failure(
            "invalid", "The OATH ID exceeds 64 bytes; choose a shorter label."
        )
    if any(c.id == data.get_id() for c in session.list_credentials()):
        raise Failure(
            "conflict", "The exact OATH ID already exists. Nothing was overwritten."
        )
    return data


def mutate(request):
    if request.get("confirm") != request["serial"]:
        raise Failure(
            "confirm_required", "Confirm the exact target serial before a write."
        )
    operation = request["op"]
    if operation in ("oath-add", "oath-delete", "oath-password", "oath-preview"):
        with oath(request) as (session, info):
            if operation in ("oath-add", "oath-preview"):
                data = validate_new_oath(session, info, request)
                if operation == "oath-preview":
                    return {
                        "message": "Target is available. Source retained; no write performed."
                    }
                credential = session.put_credential(data, touch_required=True)
                import pyotp

                timestamp = int(time.time())
                expected = pyotp.TOTP(
                    base64.b32encode(data.secret).decode(),
                    digits=data.digits,
                    interval=data.period,
                    digest=getattr(hashlib, data.hash_algorithm.name.lower()),
                ).at(timestamp)
                if (
                    session.calculate_code(credential, timestamp=timestamp).value
                    != expected
                ):
                    raise Failure(
                        "outcome_unknown",
                        "Written, but verification did not match. Source retained; inspect before retrying.",
                    )
            elif operation == "oath-delete":
                matches = [
                    c for c in session.list_credentials() if c.id.hex() == request["id"]
                ]
                if len(matches) != 1:
                    raise Failure("not_found", "Exact OATH ID no longer exists.")
                session.delete_credential(matches[0].id)
            else:
                if not request.get("new_secret"):
                    raise Failure(
                        "invalid", "A nonempty new OATH password is required."
                    )
                session.set_key(session.derive_key(request["new_secret"]))
    elif operation in ("fido-delete", "fido-pin"):
        from fido2.ctap2.pin import ClientPin

        with fido(request) as (ctap, _):
            if operation == "fido-delete":
                manager = credential_manager(ctap, request)
                if not any(c["id"] == request["id"] for c in fido_entries(manager)):
                    raise Failure("not_found", "Exact credential no longer exists.")
                manager.delete_cred(
                    {"id": bytes.fromhex(request["id"]), "type": "public-key"}
                )
            else:
                pin = ClientPin(ctap)
                if not request.get("new_secret"):
                    raise Failure("invalid", "A new PIN is required.")
                if ctap.info.options.get("clientPin"):
                    pin.change_pin(require_auth(request), request["new_secret"])
                else:
                    pin.set_pin(request["new_secret"])
    elif operation in ("piv-pin", "openpgp-pin"):
        from yubikit.core.smartcard import SmartCardConnection

        device, _ = select_device(request["serial"], SmartCardConnection)
        with device.open_connection(SmartCardConnection) as connection:
            if operation == "piv-pin":
                from yubikit.piv import PivSession

                session = PivSession(connection)
            else:
                from yubikit.openpgp import OpenPgpSession

                session = OpenPgpSession(connection)
            if not request.get("new_secret"):
                raise Failure("invalid", "A new PIN is required.")
            session.change_pin(require_auth(request), request["new_secret"])
    else:
        raise Failure(
            "invalid",
            "Unsupported operation; no generic command execution is provided.",
        )
    return {
        "message": "Completed. Refresh affected data to verify. A copied TOTP source is always retained."
    }


def execute(request):
    operation = request.get("op")
    if operation == "sources":
        return sources()
    if not str(request.get("serial", "")).isdigit():
        raise Failure("invalid", "An exact numeric device serial is required.")
    if operation == "items":
        return items(request)
    if operation == "code":
        return get_code(request)
    return mutate(request)


def main():
    from yubikit.core import InvalidPinError, NotSupportedError

    try:
        request = json.load(sys.stdin)
        result = execute(request)
        response = {"ok": True, "result": result}
    except Failure as error:
        response = {
            "ok": False,
            "error": {"code": error.code, "message": error.message},
        }
    except InvalidPinError:
        response = {
            "ok": False,
            "error": {
                "code": "pin_invalid",
                "message": "Invalid PIN/password. No automatic retry; check remaining attempts with official tools.",
            },
        }
    except NotSupportedError:
        response = {
            "ok": False,
            "error": {
                "code": "not_supported",
                "message": "This application or operation is not supported by the selected device.",
            },
        }
    except Exception:
        # CtapError codes can be classified without replaying exception text.
        error = sys.exception()
        code = getattr(error, "code", None)
        code_name = getattr(code, "name", "")
        kind = (
            "pin_invalid"
            if "PIN_INVALID" in code_name
            else ("locked" if "PIN_REQUIRED" in code_name else "backend_failed")
        )
        message = "Official backend rejected the operation. A write may have completed; inspect before retrying. Raw details suppressed."
        response = {"ok": False, "error": {"code": kind, "message": message}}
    print(json.dumps(response))


if __name__ == "__main__":
    main()
