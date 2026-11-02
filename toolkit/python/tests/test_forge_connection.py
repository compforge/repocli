import json
from pathlib import Path

import pytest

from repokit.forge import ForgeConnection, detect_provider
from repokit.forge.model import ForgeAuthError


def test_connection_identity_and_context_are_shared_with_typescript(repo, tmp_path):
    """@case Credentials, provider and endpoint isolate caches; targets stay explicit."""
    fixture = Path(__file__).resolve().parents[3] / "conformance/forge-connection.json"
    for row in json.loads(fixture.read_text()):
        connection = ForgeConnection(
            host=row["host"],
            provider=row["provider"],
            api_host=row["apiHost"],
            token=row["token"],
            state_dir=tmp_path,
        )
        assert connection.name == row["name"]
        assert connection.identity == row["identity"]
        assert row["token"] not in repr(connection)
        context = connection.pull_request_context(repo, source="owner/repo")
        assert context.source == connection.repository("owner/repo")
        assert context.targets == (context.source,)
        assert context.observer.store.root == tmp_path
        assert context.observer.reader.state_dir == tmp_path / "forge-http"
        explicit = connection.pull_request_context(
            repo, source="owner/repo", targets=("upstream/repo",)
        )
        assert explicit.targets == (connection.repository("upstream/repo"),)
        with pytest.raises(ValueError):
            connection.pull_request_context(repo, source="owner/repo", targets=())


def test_access_configuration_requires_credentials_and_valid_timeout():
    assert detect_provider("github.example") == "github"
    assert detect_provider("forge.example", "github") == "github"
    assert detect_provider("forge.example") == "gitlab"
    with pytest.raises(ForgeAuthError):
        ForgeConnection("github.com", " ")
    with pytest.raises(ValueError):
        ForgeConnection("github.com", "fixture", timeout=0)
