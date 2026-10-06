import os
import subprocess
import sys
import time
from threading import Event, Timer

import pytest

from repocli._process import Budget, run


def fake_git(tmp_path, monkeypatch, body):
    executable = tmp_path / "git"
    executable.write_text(f"#!{sys.executable}\n" + body)
    executable.chmod(0o755)
    monkeypatch.setenv("PATH", str(tmp_path))
    return tmp_path


def assert_reaped(root):
    pid = int((root / "pid").read_text())
    with pytest.raises(ProcessLookupError):
        os.kill(pid, 0)


@pytest.mark.parametrize("cancelled", [False, True])
def test_running_process_is_killed_and_reaped(tmp_path, monkeypatch, cancelled):
    root = fake_git(
        tmp_path,
        monkeypatch,
        """
import os, time
from pathlib import Path
Path('pid').write_text(str(os.getpid()))
time.sleep(30)
""",
    )
    monkeypatch.chdir(root)
    processes = []
    popen = subprocess.Popen

    def capture(*args, **kwargs):
        process = popen(*args, **kwargs)
        processes.append(process)
        return process

    monkeypatch.setattr(subprocess, "Popen", capture)
    cancel = Event()
    timer = Timer(0.3, cancel.set)
    if cancelled:
        timer.start()
    start = time.monotonic()
    try:
        with pytest.raises(InterruptedError if cancelled else TimeoutError):
            run(root, ["ls-files"], Budget(5 if cancelled else 0.3, cancel))
    finally:
        timer.cancel()
        if cancelled:
            timer.join()
    assert time.monotonic() - start < 3
    assert len(processes) == 1
    assert processes[0].returncode == -9
    with pytest.raises(ProcessLookupError):
        os.kill(processes[0].pid, 0)


@pytest.mark.parametrize("fd", [1, 2])
def test_output_overflow_reaps_process(tmp_path, monkeypatch, fd):
    root = fake_git(
        tmp_path,
        monkeypatch,
        f"""
import os, time
from pathlib import Path
Path('pid').write_text(str(os.getpid()))
os.write({fd}, b'x' * 131072)
time.sleep(30)
""",
    )
    monkeypatch.chdir(root)
    with pytest.raises(ValueError, match="output exceeds"):
        run(root, ["ls-files"], Budget(5, None), max_output=65536)
    assert_reaped(root)


def test_batch_input_and_output_do_not_deadlock(tmp_path, monkeypatch):
    root = fake_git(
        tmp_path,
        monkeypatch,
        """
import os, sys
os.write(1, b'x' * 131072)
os.write(2, b'y' * 65536)
value = sys.stdin.buffer.read()
os.write(1, value)
""",
    )
    data = b"z" * 262144
    assert run(root, ["cat-file"], Budget(5, None), input=data) == b"x" * 131072 + data


def test_nonzero_exit_and_missing_config(tmp_path, monkeypatch):
    root = fake_git(tmp_path, monkeypatch, "import sys; sys.stderr.write('failure'); sys.exit(1)")
    with pytest.raises(subprocess.CalledProcessError) as error:
        run(root, ["config"], Budget(5, None))
    assert error.value.stderr == b"failure"
    assert run(root, ["config"], Budget(5, None), allow_missing=True) == b""


def test_missing_git_is_an_error(tmp_path, monkeypatch):
    monkeypatch.setenv("PATH", str(tmp_path))
    with pytest.raises(FileNotFoundError):
        run(tmp_path, ["ls-files"], Budget(5, None))
