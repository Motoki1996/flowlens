# 0002. Why a REST API

- **Status:** Accepted
- **Date:** 2026-07-21

## Context

The web app needs a well-defined contract to the backend, and we want that
contract to be documentable and testable. Alternatives considered were
GraphQL and gRPC.

## Decision

Expose a **REST API over HTTP with JSON**, documented with **OpenAPI**.

## Consequences

- REST is simple to consume from Server Components with plain `fetch`, easy
  to test with `net/http/httptest`, and easy to document and mock.
- OpenAPI gives a single source of truth for the contract and a path to
  generated clients later.
- We avoid GraphQL's server complexity and gRPC's browser/tooling friction,
  which are not justified for FlowLens's mostly read-oriented, resource-shaped
  data.
- If a future need for flexible client-driven queries appears, a GraphQL
  layer can be added on top without discarding REST.
