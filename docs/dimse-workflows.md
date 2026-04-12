# DIMSE Workflows

## Objective

Document the current DIMSE behavior implemented for:

- professional remote search and retrieve
- patient remote search
- Synapse-specific interoperability details currently required by the product

This document reflects the current implementation and operational expectations. It is not a future-state design note.

## Scope

Current DIMSE support applies to configured PACS nodes using:

- `search.mode = "c_find"`
- `retrieve.mode = "c_move"` or `retrieve.mode = "c_get"`
- `health.mode = "dimse_c_echo"`

The current concrete Synapse node is `hpnsyn`.

## Professional DIMSE Search

Professional remote search may use DIMSE `C-FIND`.

Current behavior:

- the physician UI can explicitly target a configured remote PACS node
- when the selected node uses `search.mode = "c_find"`, the backend executes study-level `C-FIND` through Orthanc
- returned studies are normalized into the same physician result contract used by QIDO-backed nodes

## Orthanc / Synapse Parsing

The backend must tolerate Orthanc/Synapse `C-FIND` answers in the nested tag-object form:

```json
{
  "0020,000d": {
    "Name": "StudyInstanceUID",
    "Type": "String",
    "Value": "1.2.3..."
  }
}
```

Operational rule:

- tag lookup must be case-insensitive enough for Orthanc JSON variants
- values may be carried in nested `Value` objects, not only as plain strings

Without this tolerance, valid Synapse studies can be decoded as if they were missing `StudyInstanceUID`.

## Professional Retrieve From DIMSE Search Results

Searching by `C-FIND` is not enough by itself.

For professional retrieve to work from DIMSE search results:

- the backend must persist remote search results in the shared QIDO/DIMSE cache
- the backend must persist physician recent-query context
- retrieve origin resolution must not depend only on one field path if equivalent source data is available

Current operational rule:

- professional retrieve from DIMSE results is supported only because `C-FIND` results are now persisted and source-node resolution falls back safely when needed

Expected behavior:

- a study found through physician DIMSE search can later be retrieved without losing its origin PACS context

## Patient DIMSE Search

Patient remote search may now also probe DIMSE nodes.

This is independent from patient identity resolution itself.

Patient identity lookup remains a separate concern. DIMSE search only determines whether remote studies should be exposed for an already validated patient.

## Patient DIMSE Discovery Strategy

Patient DIMSE search uses two discovery paths and merges candidates by `StudyInstanceUID`:

1. identifier-based discovery
2. demographic discovery

### 1. Identifier-Based Discovery

The backend probes DIMSE nodes using all patient identifiers already known for the patient, including:

- canonical document number
- alternate identifiers persisted for the patient
- current Mongo-backed `_id` alias when available

This path is used to maximize recall when remote `PatientID` carries one of the portal-known identifiers.

### 2. Demographic Discovery

When enough patient data is available, the backend also issues a demographic `C-FIND`.

Current required fields:

- `PatientBirthDate`
- `PatientSex`
- `PatientName`

Current Synapse-compatible name strategy:

- use surname wildcard, for example `CASTRO*`
- do not send the old full-name fuzzy formatting for this query path

Current Synapse-compatible value normalization:

- birth date must be normalized to `YYYYMMDD`
- sex must be normalized to DICOM single-letter form (`F`, `M`, `O`)

## Patient DIMSE Authorization Rules

Discovery and authorization are separate.

Current authorization order:

1. strict identifier match
2. strong demographic match

### Rule 1. Identifier Match

A candidate study is authorized when remote `PatientID` matches one of the patient identifiers known by the portal.

This is the strongest current rule.

### Rule 2. Demographic Match

If no identifier match is available, a study may still be authorized when there is a strong demographic match combining:

- exact normalized birth date
- exact normalized sex
- high-confidence patient-name match

Current implementation is intentionally conservative.

## Async Patient Search Worker

Patient background search workers must reload the full patient summary from the database before running remote search.

Reason:

- demographic DIMSE search requires complete patient data
- only carrying `patient_id` and `document_number` is insufficient

Operational rule:

- background search jobs must not reconstruct a partial `PatientSummary` ad hoc

## Synapse Timeouts

Demographic `C-FIND` queries against Synapse can take materially longer than basic identifier probes.

Current operational rule:

- Orthanc-mediated `C-FIND` HTTP calls must use a longer dedicated timeout than the generic backend HTTP client

This applies to the full query-answer-content sequence:

- `POST /modalities/{id}/query`
- `GET /queries/{id}/answers`
- `GET /queries/{id}/answers/{n}/content`

## Logging

Temporary diagnostic logs were used while stabilizing Synapse interoperability.

Current desired state:

- temporary one-off debugging noise should be removed once the workflow is stable
- persistent logs should focus on operationally useful milestones and errors

## Non-Goals

This document does not define:

- future metadata-sequence matching from DICOM metadata
- final feature-specific HIS provider split
- future REST replacement for current Mongo-backed integrations

Those remain separate work items.
