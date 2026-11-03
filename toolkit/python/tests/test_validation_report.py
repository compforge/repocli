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
            "fingerprint": "sha256:inputs",
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


@pytest.mark.parametrize(
    "status,complete,passed",
    [
        ("passed", True, True),
        ("failed", True, False),
        ("skipped", True, True),
        ("unavailable", True, False),
        ("cancelled", False, False),
        ("uncertain", False, False),
    ],
)
def test_aggregate_outcome_matches_checks(tmp_path, status, complete, passed):
    """@case Known failure is valid evidence; contradictory aggregate flags are not."""
    value = report(tmp_path)
    value["components"][0]["checks"][0]["status"] = status
    value.update(complete=complete, passed=passed)
    assert decode_validation_report(json.dumps(value), checkout=tmp_path) == value
    for flag in ("complete", "passed"):
        invalid = copy.deepcopy(value)
        invalid[flag] = not invalid[flag]
        with pytest.raises(ValueError, match="contradicts"):
            decode_validation_report(json.dumps(invalid), checkout=tmp_path)


def test_no_selected_tasks_is_distinct_from_empty_component_results(tmp_path):
    value = report(tmp_path)
    value["components"][0]["checks"] = []
    with pytest.raises(ValueError, match="omitted requested checks"):
        decode_validation_report(json.dumps(value), checkout=tmp_path)
    value["components"] = []
    assert decode_validation_report(json.dumps(value), checkout=tmp_path) == value


def test_changed_inputs_cannot_claim_validation_passed(tmp_path):
    value = report(tmp_path)
    value["inputsConsistent"] = False
    with pytest.raises(ValueError, match="contradicts"):
        decode_validation_report(json.dumps(value), checkout=tmp_path)
    value["passed"] = False
    assert decode_validation_report(json.dumps(value), checkout=tmp_path) == value


@pytest.mark.parametrize(
    "status,complete,passed,checks",
    [
        ("passed", True, True, ("format",)),
        ("unavailable", True, False, ("format",)),
        ("failed", True, False, ("format", "lint", "test")),
        ("cancelled", False, False, ("format", "lint", "test")),
        ("passed", False, False, ("format", "lint", "test")),
    ],
)
def test_format_only_and_early_termination_are_valid(tmp_path, status, complete, passed, checks):
    """@case Format can stop before validation and does not promise input consistency."""
    value = report(tmp_path)
    value.update(
        components=[],
        preparation=[{"checks": [{"check": "format", "status": status}]}],
        inputsConsistent=False,
        complete=complete,
        passed=passed,
    )
    assert decode_validation_report(json.dumps(value), checkout=tmp_path, checks=checks) == value


def test_failed_preparation_cannot_be_followed_by_successful_checks(tmp_path):
    value = report(tmp_path)
    value["preparation"] = [{"checks": [{"check": "format", "status": "failed"}]}]
    with pytest.raises(ValueError, match="unsuccessful preparation"):
        decode_validation_report(
            json.dumps(value), checkout=tmp_path, checks=("format", "lint", "test")
        )


@pytest.mark.parametrize("checks", [("lint", "test"), ("format", "lint", "test")])
def test_stale_plan_can_abort_before_any_checks(tmp_path, checks):
    """@case Inputs changed after planning produces an incomplete, empty report."""
    value = report(tmp_path)
    value.update(
        components=[],
        inputsConsistent=False,
        complete=False,
        passed=False,
        diagnostics=["inputs changed after planning; no checks executed"],
    )
    assert decode_validation_report(json.dumps(value), checkout=tmp_path, checks=checks) == value


def test_check_fingerprint_is_required_without_legacy_alias(tmp_path):
    value = report(tmp_path)
    decoded = decode_validation_report(json.dumps(value), checkout=tmp_path)
    assert decoded["components"][0]["checks"][0]["fingerprint"] == "sha256:inputs"
    for check in value["components"][0]["checks"]:
        check["inputFingerprint"] = check.pop("fingerprint")
    with pytest.raises(ValueError, match="incomplete validation check result"):
        decode_validation_report(json.dumps(value), checkout=tmp_path)
