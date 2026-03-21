# Human Decisions Log

Use this file to record the decisions you make after reviewing the agent discussion.

## Status
- In progress

## Decisions
- MVP first, before auth features.
- The first deliverable must be a Docker Compose stack.
- The stack must include `orthanc`, `backend`, `postgres`, `nginx`, and `ohif`.
- Local Orthanc is mandatory and acts as the cache and retrieve target.
- PostgreSQL is the default persistence layer for operational data unless a clearly better fit is justified during the design discussion.
- No doctor login in the MVP.
- No patient login in the MVP.
- No OTP, sessions, JWT, or share links in the MVP.
- HIS integration will be configured from values provided later by the user.
- Remote dcm4chee connection details will be provided later by the user and must be externalized as configuration.

## Open Inputs From User
- HIS base URL, authentication method, API key format, and required query parameters.
- Remote dcm4chee nodes: hostnames, ports, AE Titles, DICOMweb base URLs, and credentials.
- Preferred public hostnames or local ports for Nginx exposure.

## Questions To Revisit During Spec
- Is PostgreSQL sufficient for cached metadata, or should cache state live partly in Orthanc and partly in Postgres?
- Should the MVP support only manual retrieve, or also background prefetch jobs without user auth?

## Notes For Agents
- Prefer pragmatic, secure defaults.
