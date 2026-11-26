"""Canonical relationship type names; directions are writer requirements."""

from enum import StrEnum


class RelationshipType(StrEnum):
    """Shared relationship vocabulary for graph writers and queries."""

    ISSUED = "ISSUED"
    BELONGS_TO = "BELONGS_TO"
    IN_SECTOR = "IN_SECTOR"
    OPERATES_IN = "OPERATES_IN"
    SUPPLIES = "SUPPLIES"
    CUSTOMER_OF = "CUSTOMER_OF"
    COMPETES_WITH = "COMPETES_WITH"
    PRODUCES = "PRODUCES"
    AFFECTED_BY = "AFFECTED_BY"
    IMPACTS = "IMPACTS"
    REPORTED_IN = "REPORTED_IN"
    ABOUT = "ABOUT"
    SUBJECT = "SUBJECT"
    OBJECT = "OBJECT"
    SUPPORTED_BY = "SUPPORTED_BY"
    SUPERSEDES = "SUPERSEDES"
    DISPUTES = "DISPUTES"
