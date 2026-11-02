import copy
import json

import pytest

from repokit import decode_validation_report


def report(checkout):
    checks = [
        {
            "check": name,
            "requestedComponents": ["."],
            "status": "passed",
            "scope": "full",
            "command": ["make", name],
            "files": [],
            "reason": "fixture",
            "reused": False,
            "reuseReason": "no previous check",
            "checkedAt": "2026-11-02T00:00:00Z",
            "inputFingerprint": "sha256:inputs",
            "environment": "test",
            "witnesses": {},
            "stdout": "",
            "stderr": "",
        }
        for name in ("lint", "test")
    ]
    return {
        "schemaVersion": 1,
        "checkout": str(checkout),
        "snapshot": "sha256:snapshot",
        "inputsConsistent": True,
        "complete": True,
        "passed": True,
        "components": [{"component": {"root": "."}, "checks": checks}],
        "diagnostics": [],
    }


def test_failed_report_and_partial_fallback_groups_are_valid(tmp_path):
    value = report(tmp_path)
    lint, test = value["components"][0]["checks"]
    lint["requestedComponents"] = test["requestedComponents"] = ["child"]
    test["status"] = "failed"
    value["passed"] = False
    value["components"] = [
        {"component": {"root": "."}, "checks": [lint]},
        {"component": {"root": "child"}, "checks": [test]},
    ]
    assert (
        decode_validation_report(
            json.dumps(value), checkout=tmp_path, checks=("format", "lint", "test")
        )
        == value
    )


@pytest.mark.parametrize(
    "mutation", ["checkout", "schema", "missing", "duplicate", "escape", "shape", "bool"]
)
def test_invalid_report_is_not_validation_evidence(tmp_path, mutation):
    value = report(tmp_path)
    if mutation == "checkout":
        value["checkout"] = str(tmp_path / "other")
    elif mutation == "schema":
        value["schemaVersion"] = 2
    elif mutation == "missing":
        value["components"][0]["checks"].pop()
    elif mutation == "duplicate":
        value["components"] *= 2
    elif mutation == "escape":
        value["components"][0]["component"]["root"] = "../outside"
    elif mutation == "shape":
        value["components"] = [None]
    else:
        value["complete"] = 1
    with pytest.raises(ValueError):
        decode_validation_report(json.dumps(value), checkout=tmp_path)


@pytest.mark.parametrize("sources", [[["child"]], [{"root": "."}], [1], [".", "."], []])
def test_requested_component_identity_must_be_unique_strings(tmp_path, sources):
    value = copy.deepcopy(report(tmp_path))
    value["components"][0]["checks"][0]["requestedComponents"] = sources
    with pytest.raises(ValueError, match="requestedComponents"):
        decode_validation_report(json.dumps(value), checkout=tmp_path)
