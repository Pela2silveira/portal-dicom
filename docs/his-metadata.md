# HIS Metadata By PACS Node

## Objective

Document the current contract for node-level HIS metadata in PACS configuration, the current runtime behavior, and the intended separation between:

- patient/professional identity resolution
- study metadata enrichment (`Prestación en ANDES`, `Profesional en ANDES`)

## Current Model

Each PACS node may declare:

- `his: true|false`
- `tipoPrestacion: []`

Example:

```json
{
  "id": "hpn",
  "name": "Hospital Provincial Neuquén",
  "his": true,
  "tipoPrestacion": [
    {
      "conceptId": "77477000",
      "term": "tomografía axial computarizada",
      "nombre": "tomografía axial computarizada",
      "fsn": "tomografía axial computarizada (procedimiento)",
      "semanticTag": "procedimiento"
    }
  ]
}
```

## Semantics

### `his`

`his` is a node-level functional flag.

- `his: true`
  - the node belongs to the HIS/ANDES integration domain
  - study-level ANDES metadata may be requested for studies coming from this node
- `his: false`
  - the node is outside that metadata domain
  - the portal must not attempt study-level ANDES enrichment for studies coming from this node

This flag does not define the technical transport used to query HIS.

It only answers:

- does HIS/ANDES study metadata apply to this node?

### `tipoPrestacion`

`tipoPrestacion` stores procedure metadata associated with the PACS node as obtained from external HIS/Mongo metadata.

At the moment it is configuration data only:

- it is parsed by backend config models
- it is exposed in config payloads
- it is not yet used by search, filtering, ranking, or UI badges

## Runtime Behavior

### Patient and Professional Cards

For studies/results coming from `his: false` nodes:

- `Prestación en ANDES` must render as `n/a`
- `Profesional en ANDES` must render as `n/a`

For studies/results coming from `his: true` nodes:

- the UI renders the resolved value when present
- otherwise it renders `-`

### Backend Enrichment

For studies/results coming from `his: false` nodes:

- the backend must not attempt to enrich from ANDES/HIS
- this applies independently of whether the current implementation uses Mongo or REST

For studies/results coming from `his: true` nodes:

- the backend may attempt ANDES/HIS enrichment according to the current implementation and feature scope

### Current REST Provider Behavior (Operational)

When `his.prestaciones_provider = "rest"` and `his.prestaciones_enrichment_enabled = true`:

- REST endpoint used: `GET /modules/rup/prestaciones`
- auth header: `Authorization: JWT <token>`
- base URL source: `HIS_BASE_URL` env var (default `https://app.andes.gob.ar/api`)
- request timeout source: `his.andes_rest_request_timeout_ms` (current local default `3000`)

Current request strategy:

- patient flow:
  - resolve one patient `mongo_object_id`
  - perform one REST call for that patient
  - map returned `metadata.pacs-uid` to `StudyInstanceUID`
- professional flow:
  - group candidate studies by patient (`mongo_object_id`)
  - perform one REST call per grouped patient
  - process grouped patients in parallel worker-style (bounded by `his.andes_rest_concurrency`)
  - never block HTTP search response; enrichment/persistence runs in background queue
  - local cache searches now follow the same pattern: first overlay persisted `qido_study_cache` metadata, then enqueue background enrichment for studies still missing `andes_*`

Current known operational caveats:

- if REST is slow and timeout is too aggressive, enrichment success rate drops (no UI block, but fewer persisted `andes_*` fields)
- some studies exist in ANDES but not in `estado=validada`; those are currently excluded from match
- for professional flow, some DICOM `PatientID` values cannot be resolved to a Mongo `_id` and are skipped
- for professional DIMSE (`c_find`) sources, node-level timeouts are treated as degraded node responses (empty results) instead of failing the whole HTTP request

### Current Prestaciones Resolution

The current Mongo `prestaciones` integration resolves study metadata through the DICOM study identifier, not through patient/date narrowing.

Current rule:

- `metadata.key = "pacs-uid"`
- `metadata.valor = StudyInstanceUID`

Current persisted study-level outputs:

- `andes_prestacion_id`
- `andes_prestacion`
- `andes_professional`
- `andes_prestacion_id` is also used to enable report download in both flows (patient/professional) (`POST /modules/descargas` -> Base64 PDF)

Implications:

- `tipoPrestacion` remains node metadata, not study metadata
- `tipoPrestacion` is not copied into Postgres study rows
- the current enrichment path is compatible with the existing `qido_study_cache` fields and does not require a new Postgres table
- the Mongo lookup is batched by `StudyInstanceUID` so large result sets do not depend on a single oversized `$in` query

## Important Separation

Patient identity resolution is independent from the PACS node.

This means:

- patient lookup/auth can still use a global HIS provider
- study metadata enrichment can still be skipped per node based on `his`

So these two concerns must remain separate:

1. who is the patient/professional
2. whether a study from a given node should carry ANDES metadata

## Provider Direction

The current system may use different technical implementations over time:

- direct Mongo read for some features
- REST API for other features

This does not change the meaning of `his`.

`his` is provider-agnostic.

The transport/provider decision belongs to feature implementation, not to the node-level semantic contract.

## Current Helpers

Backend currently centralizes this through availability helpers rather than scattered `his` checks.

Frontend currently centralizes this through a metadata availability helper for card rendering.

These helpers must remain semantic, not transport-specific.

Bad examples:

- `nodeUsesMongo`
- `showAndesFromApi`

Good examples:

- `andesMetadataAvailable...`
- `sourceNodeUsesHIS`

## Pending Work

- define whether `tipoPrestacion` will participate in UI filters or visual badges
- define feature-specific HIS providers if identity and prestaciones diverge by transport
- when prestaciones move to REST, keep the same `his` semantics and replace only the underlying implementation
- decide final `estado` strategy for REST lookup (`validada` only vs multiple states)
- tune timeout/concurrency per environment to balance latency and enrichment hit-rate
