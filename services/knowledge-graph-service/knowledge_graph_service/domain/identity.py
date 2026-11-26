"""Canonical identifier formatting, not alias/entity-resolution or graph ingestion."""

import re
from enum import StrEnum


class NodeLabel(StrEnum):
    COMPANY = "Company"
    INSTRUMENT = "Instrument"  # Company share
    MARKET_EVENT = "MarketEvent"
    SECTOR = "Sector"  # Economy related area
    INDUSTRY = "Industry"
    PRODUCT = "Product"
    SOURCE = "Source"
    CLAIM = "Claim"


def canonical_company_id(cik: str) -> str:
    value = cik.strip()
    if not re.fullmatch(r"[0-9]{1,10}", value) or int(value) == 0:
        raise ValueError("CIK must contain 1–10 ASCII decimal digits and a nonzero value")
    return f"cik:{value.zfill(10)}"


def canonical_instrument_id(share_class_figi: str) -> str:
    value = share_class_figi.strip().upper()
    if not re.fullmatch(r"BBG[A-Z0-9]{9}", value):
        raise ValueError("a provider-backed share-class FIGI is required, not a ticker")
    return f"figi:{value}"


def scoped_key(label: NodeLabel, identifier: str, scope: str = "global") -> str:
    if not isinstance(label, NodeLabel):
        raise ValueError("unknown graph label")
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._:-]{0,255}", identifier):
        raise ValueError("invalid stable graph identifier")
    if scope != "global" and not re.fullmatch(r"tenant:[A-Za-z0-9][A-Za-z0-9._:-]{0,127}", scope):
        raise ValueError("scope must be global or an explicitly authenticated tenant scope")
    if label in {NodeLabel.COMPANY, NodeLabel.INSTRUMENT, NodeLabel.SECTOR, NodeLabel.INDUSTRY}:
        if scope != "global":
            raise ValueError("canonical public catalog identities are global, not private facts")
    return f"{scope}|{label.value}|{identifier}"
