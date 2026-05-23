"""Schema validation tests."""

from __future__ import annotations

from datetime import datetime, timezone

import pytest
from pydantic import ValidationError as PydanticValidationError

from fact0.audit.models import Event


def _valid_kwargs(**overrides) -> dict:
    base = {
        "actor": {"id": "u1", "type": "human", "email": "u@x.com"},
        "action": "doc.read",
        "resource": {"id": "doc_1", "type": "document", "name": "x"},
        "outcome": "success",
    }
    base.update(overrides)
    return base


def test_valid_event_round_trips_json():
    e = Event(**_valid_kwargs())
    j = e.model_dump(exclude_none=True, mode="json")
    assert j["action"] == "doc.read"
    assert j["actor"]["type"] == "human"
    assert j["outcome"] == "success"


@pytest.mark.parametrize(
    "field,value,err",
    [
        ("actor", {"id": "", "type": "human"}, "actor"),
        ("actor", {"id": "u", "type": "robot"}, "type"),
        ("action", "", "action"),
        ("resource", {"id": "", "type": "document"}, "resource"),
        ("resource", {"id": "r", "type": ""}, "resource"),
        ("outcome", "ok", "outcome"),
    ],
)
def test_validation_rejects_bad_field(field, value, err):
    kwargs = _valid_kwargs(**{field: value})
    with pytest.raises(PydanticValidationError) as ex:
        Event(**kwargs)
    assert err in str(ex.value).lower()


def test_extra_fields_forbidden():
    with pytest.raises(PydanticValidationError):
        Event(**_valid_kwargs(), oops="no")


def test_timestamp_serialised_as_isoformat():
    ts = datetime(2026, 5, 15, 10, 23, 44, 0, tzinfo=timezone.utc)
    e = Event(**_valid_kwargs(timestamp=ts))
    j = e.model_dump(exclude_none=True, mode="json")
    assert j["timestamp"].startswith("2026-05-15T10:23:44")
