# ADR-0001: Service architecture

- Status: Accepted
- Decision: Domain-Driven Design at the system boundary; Clean Architecture inside
  each service; event-driven integration through Kafka.
- Internal dependency direction: Presentation -> Application -> Domain.
  Infrastructure implements ports defined by Application/Domain and is wired at
  the composition root.
- LangGraph belongs to application orchestration/integration, not the domain model.
- CQRS is introduced only where read/write workloads or models justify it.
- Consequences: services own their data; cross-service writes happen through
  APIs or events, never direct database access.
