"""Pydantic models for audit events."""

from __future__ import annotations

from datetime import datetime, timezone
from enum import Enum
from typing import Any, Optional

from pydantic import BaseModel, ConfigDict, Field, field_serializer


class ActorType(str, Enum):
    HUMAN = "human"
    AGENT = "agent"
    SYSTEM = "system"


class Outcome(str, Enum):
    SUCCESS = "success"
    FAILURE = "failure"
    ERROR = "error"


class Actor(BaseModel):
    model_config = ConfigDict(extra="forbid")
    id: str = Field(min_length=1)
    type: ActorType
    email: Optional[str] = None


class Resource(BaseModel):
    model_config = ConfigDict(extra="forbid")
    id: str = Field(min_length=1)
    type: str = Field(min_length=1)
    name: Optional[str] = None


class Event(BaseModel):
    """Wire shape of an audit event."""

    model_config = ConfigDict(extra="forbid")

    actor: Actor
    action: str = Field(min_length=1)
    resource: Resource
    outcome: Outcome
    metadata: dict[str, Any] = Field(default_factory=dict)
    id: Optional[str] = None
    timestamp: Optional[datetime] = None

    @field_serializer("timestamp")
    def _serialize_ts(self, v: Optional[datetime]) -> Optional[str]:
        if v is None:
            return None
        if v.tzinfo is None:
            v = v.replace(tzinfo=timezone.utc)
        return v.isoformat()
