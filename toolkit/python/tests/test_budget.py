import io
import sys
from threading import Event
from unittest.mock import patch

import pytest

from repocli import git, operation
from repocli.budget import current_budget
from repocli.forge._rest import RestClient
from repocli.forge.model import ForgeError, ForgeOutcomeUnknown


def test_unknown_write_response_is_not_retried():
    client = RestClient("https://example.invalid", {})
    calls = []

    def applied(request, **kwargs):
        calls.append(request.method)
        return io.BytesIO(b"not json after server applied write")

    with patch("urllib.request.urlopen", applied):
        with pytest.raises(ForgeOutcomeUnknown, match="inspect remote state"):
            client.post("pulls", {"title": "test"})
        with pytest.raises(ForgeError) as error:
            client.get("pulls")
        assert not isinstance(error.value, ForgeOutcomeUnknown)
    assert calls == ["POST", "GET"]


def test_pagination_uses_one_deadline():
    client = RestClient("https://example.invalid", {}, timeout=10)
    clock = [0.0]
    calls = []

    def get(path, **params):
        calls.append(params["page"])
        clock[0] += 6
        return [1] * 100

    with (
        patch("repocli._process.time.monotonic", lambda: clock[0]),
        patch.object(client, "get", get),
    ):
        with pytest.raises(TimeoutError):
            client.get_all("pulls")
    assert calls == [1, 2]


def test_nested_budget_cannot_reset_deadline_or_cancellation():
    cancel = Event()
    with operation(timeout=5, cancel=cancel):
        outer = current_budget(100).deadline
        with operation(timeout=100):
            assert current_budget(100).deadline <= outer
            cancel.set()
            with pytest.raises(InterruptedError):
                current_budget(100).remaining()
    assert current_budget(1).remaining() > 0


def test_cancel_before_git_launch_is_known_non_action(repo):
    cancel = Event()
    with operation(timeout=5, cancel=cancel):
        cancel.set()
        result = git.commit(repo, "must not run")
    assert not result.ok and not result.uncertain


def test_git_timeout_after_effect_is_uncertain_and_process_reaped(tmp_path):
    marker = tmp_path / "effect"
    with operation(timeout=0.2):
        result = git._run(
            [
                sys.executable,
                "-c",
                "import pathlib,time; "
                "pathlib.Path(__import__('sys').argv[1]).write_text('done'); time.sleep(10)",
                str(marker),
            ],
            30,
        )
    assert marker.read_text() == "done"
    assert not result.ok and result.uncertain


def test_git_output_is_bounded_and_success_stderr_preserved():
    result = git._run([sys.executable, "-c", "import sys;sys.stderr.write('warning')"], 5)
    assert result.ok and result.err == "warning"
    result = git._run([sys.executable, "-c", "import sys;sys.stdout.write('x'*(17<<20))"], 5)
    assert not result.ok and result.uncertain and "exceeds" in result.err
