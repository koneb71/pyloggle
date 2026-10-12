"""The configuration page in the docs lists every ClientConfig option, and nothing else."""

import dataclasses
import re
from pathlib import Path

import pytest

from pyloggle import ClientConfig

DOC = Path(__file__).resolve().parents[2] / "docs" / "send-data" / "configuration.md"
# Documented on the page but not ClientConfig fields.
INIT_ONLY = {"integrations"}


@pytest.mark.skipif(not DOC.exists(), reason="the docs are not part of this checkout")
def test_configuration_page_lists_every_option():
    text = DOC.read_text(encoding="utf-8")
    documented = set(re.findall(r"^\| `([a-z_]+)` \|", text, flags=re.M))
    fields = {f.name for f in dataclasses.fields(ClientConfig)}
    assert fields - documented == set(), "options missing from docs/send-data/configuration.md"
    assert documented - fields - INIT_ONLY == set(), "docs/send-data/configuration.md lists options ClientConfig does not have"
