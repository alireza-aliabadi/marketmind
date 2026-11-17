"""M3.1 label/relationship specification; no claim ingestion or resolution behavior."""

from dataclasses import dataclass
from enum import StrEnum

from knowledge_graph_service.domain.identity import NodeLabel


class RelationshipType(StrEnum):
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


@dataclass(frozen=True)
class RelationshipRule:
    sources: frozenset[NodeLabel]
    targets: frozenset[NodeLabel]
    material: bool
    symmetric: bool = False


def labels(*values: NodeLabel) -> frozenset[NodeLabel]:
    return frozenset(values)


COMPANY = labels(NodeLabel.COMPANY)
CLAIM = labels(NodeLabel.CLAIM)
SOURCE = labels(NodeLabel.SOURCE)
EVENT = labels(NodeLabel.MARKET_EVENT)
ENTITIES = labels(
    NodeLabel.COMPANY,
    NodeLabel.INSTRUMENT,
    NodeLabel.SECTOR,
    NodeLabel.INDUSTRY,
    NodeLabel.PRODUCT,
    NodeLabel.MARKET_EVENT,
)

RELATIONSHIPS: dict[RelationshipType, RelationshipRule] = {
    RelationshipType.ISSUED: RelationshipRule(COMPANY, labels(NodeLabel.INSTRUMENT), True),
    RelationshipType.BELONGS_TO: RelationshipRule(COMPANY, labels(NodeLabel.INDUSTRY), True),
    RelationshipType.IN_SECTOR: RelationshipRule(
        labels(NodeLabel.INDUSTRY), labels(NodeLabel.SECTOR), True
    ),
    RelationshipType.OPERATES_IN: RelationshipRule(
        COMPANY, labels(NodeLabel.SECTOR, NodeLabel.INDUSTRY), True
    ),
    RelationshipType.SUPPLIES: RelationshipRule(COMPANY, COMPANY, True),
    RelationshipType.CUSTOMER_OF: RelationshipRule(COMPANY, COMPANY, True),
    RelationshipType.COMPETES_WITH: RelationshipRule(COMPANY, COMPANY, True, symmetric=True),
    RelationshipType.PRODUCES: RelationshipRule(COMPANY, labels(NodeLabel.PRODUCT), True),
    RelationshipType.AFFECTED_BY: RelationshipRule(ENTITIES - EVENT, EVENT, True),
    RelationshipType.IMPACTS: RelationshipRule(EVENT, ENTITIES - EVENT, True),
    RelationshipType.REPORTED_IN: RelationshipRule(EVENT, SOURCE, True),
    RelationshipType.ABOUT: RelationshipRule(SOURCE | EVENT, ENTITIES, True),
    RelationshipType.SUBJECT: RelationshipRule(CLAIM, ENTITIES, False),
    RelationshipType.OBJECT: RelationshipRule(CLAIM, ENTITIES, False),
    RelationshipType.SUPPORTED_BY: RelationshipRule(CLAIM, SOURCE, False),
    RelationshipType.SUPERSEDES: RelationshipRule(CLAIM, CLAIM, False),
    RelationshipType.DISPUTES: RelationshipRule(CLAIM, CLAIM, False),
}
