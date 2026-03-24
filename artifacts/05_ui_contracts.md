# 05 UI Contracts

## Purpose

This artifact defines the explicit UI contracts for the two future portal-owned access surfaces:

- patient study list
- physician search and workflow panel

These contracts are intentionally separate from OHIF. OHIF remains a viewer surface only.

## Global Rules

- The portal is the primary access surface for both actors.
- The public landing page must not expose direct operational links to OHIF root, backend health routes, or raw DICOMweb routes.
- OHIF is opened only for a specific authorized study or series.
- Hiding the native OHIF study list is a UX choice and is not a security control.
- Real access control, when implemented, must be enforced in the backend and image proxy by active portal session and allowed `StudyInstanceUID`.
- All portal-owned surfaces must be responsive and usable on mobile for consultation workflows.

## Patient Contract

### Purpose

Allow a patient to see only their authorized studies and open one selected study in OHIF.

### Entry Surface

- Public landing flow: `Documento + código por mail`
- Current implementation already opens the portal-owned patient surface.
- The visible language of the entry flow should read as product, not as internal demo text.
- The institutional strapline in the public header should read `Ministerio de Salud - Provincia del Neuquén`.
- The public landing should keep the access form as the visual center and leave only brief orientation copy around it.
- The supporting public copy should converge into a single compact visual element instead of duplicating the role descriptions already present in the selector.
- The public orientation message should live in the top band of the login panel, not as an independent sibling block outside it.
- The orientation message may use a lighter accent band inside the login panel to separate it from the role selector without becoming a competing primary call to action.
- The login panel should prefer short labels and should avoid explanatory helper paragraphs unless they change the next action.
- The login panel should avoid redundant internal banners or badges that restate the access context without changing the next action.
- Institutional links may appear in the footer as secondary navigation, provided they do not compete visually with the access form. The current shared footer keeps `Salud Neuquén` and the Android app link available across login, patient, and physician surfaces.
- Institutional logos such as ANDES and RedTICS may appear in the footer as secondary brand references and do not need to be interactive.
- Future real implementation validates patient identity before exposing the list.

### Screen Model

- Compact patient data summary section at the top of the page
- Simple filters in a dedicated block below the patient data section
- Authorized study list grouped by modality in a full-width results block as the primary visual element
- Per-study actions: `Recuperar estudio` or `Ver estudio`
- Empty state message when the document has no matching studies

### Allowed Fields In The List

- `studyInstanceUID`
- `studyDate`
- `modalitiesInStudy`
- `studyDescription`
- `availabilityStatus`

### Patient Summary Fields

- `fullName`
- `documentNumber`
- `birthDate`
- `sex`
- `genderIdentity`

### Allowed Filters

- `period`
  values: `today`, `week`, `month`, `year`, `custom`, or empty for all dates
  behavior: the dropdown presets must set the patient date range automatically
  default: `month`
- `dateRange`
  behavior: a calendar range picker inside an inline dropdown allows first click = start date and second click = end date, with the selected interval highlighted
  layout: the date preset selector and range dropdown should sit horizontally within the shared date filter block when space allows
- `modality`
  values: `all` or one enumerated public-facing modality at a time, shown to the patient with plain-language labels in Spanish
- free text is out of scope for the first patient surface

### Allowed Sort

- default sort: newest `studyDate` first
- grouping: first by primary modality, then by newest `studyDate` inside each group

### Availability States

- `available_local`: study is already in local Orthanc and can be opened now
- `pending_retrieve`: study is expected but not yet available for viewing
- `unavailable`: study exists in patient authorization scope but is not currently retrievable
- `error`: retrieval or authorization resolution failed

### Allowed Actions

- `Recuperar estudio` when `availabilityStatus = pending_retrieve`
- `Ver estudio` when `availabilityStatus = available_local`
- `Buscar` to reload studies with the current patient filters

### Explicitly Forbidden Actions

- free search over all studies in cache
- visibility of remote PACS nodes
- direct navigation through the native OHIF study list

### OHIF Handoff

- Portal opens OHIF for a specific `studyInstanceUID`
- The current portal UX opens the viewer in a new browser tab
- OHIF should not expose a global study list in the final patient flow

### Backend Contract For Patient Surface

- `GET /api/patient/studies`
  - returns only studies authorized for the active patient session
- `POST /api/patient/retrieve`
  - receives `document_number` + `study_instance_uid`
  - triggers PACS-to-PACS retrieve through Orthanc REST
  - updates local availability before the patient can open OHIF
- `GET /api/patient/studies/:studyInstanceUID/access`
  - returns whether the session can open the study and the viewer route or token material needed by the final design

### Future Security Constraint

- Backend and proxy must restrict OHIF/image access by active session and allowed `StudyInstanceUID`
- Patient visibility in the portal is necessary but not sufficient without this enforcement

## Physician Contract

### Purpose

Allow a physician to search, inspect, and retrieve studies from remote PACS nodes, then open a selected study in OHIF.

### Entry Surface

- Public landing flow: `DNI / usuario + contraseña`
- Current implementation already opens the portal-owned physician surface
- The visible language of the entry flow should read as operational product language, not as internal demo text
- The institutional strapline in the public header should read `Ministerio de Salud - Provincia del Neuquén`.
- The public landing should keep supporting context concise so the professional access form remains the dominant visual element.
- The supporting public copy should converge into a single compact visual element instead of restating the patient and professional descriptions outside the selector.
- The public orientation message should live in the top band of the login panel, not as an independent sibling block outside it.
- The orientation message may use a lighter accent band inside the login panel to separate it from the role selector without becoming a competing primary call to action.
- The login panel should prefer short labels and should avoid explanatory helper paragraphs unless they change the next action.
- The login panel should avoid redundant internal banners or badges that restate the access context without changing the next action.
- Institutional links may appear in the footer as secondary navigation, provided they do not compete visually with the access form. The current shared footer keeps `Salud Neuquén` and the Android app link available across login, patient, and physician surfaces.
- Institutional logos such as ANDES and RedTICS may appear in the footer as secondary brand references and do not need to be interactive.
- Future real implementation target: `LDAP provincial + MFA`

### Screen Model

- Search filter bar
- Search execution status
- Federated results table
- Context panels that anticipate future capabilities: multi-node search, operational state, and asynchronous follow-up
- Per-study actions
- Optional retrieve job activity summary

### Search Filters

- `patient_id`
- `patient_name`
- `date_from`
- `date_to`
- `modalities`
- filters should start empty by default in the current portal UX

### Result Columns

- `patientName`
- `patientID`
- `studyDate`
- `studyTime`
- `modalitiesInStudy`
- `studyDescription`
- `locations[]`
- `cacheStatus`
- `retrieveStatus`
- `partialFilter`

### Result Semantics

- One logical row per deduplicated `StudyInstanceUID`
- `locations[]` accumulates all remote nodes where the study exists
- Preferred metadata source follows the existing node priority rule
- Cache state is shown separately from descriptive study metadata

### Cache States

- `not_local`
- `local_partial`
- `local_complete`

### Retrieve States

- `idle`
- `queued`
- `running`
- `done`
- `failed`

### Allowed Actions

- `Buscar`
- `Recuperar estudio`
- `Reintentar recuperacion` when the latest retrieve status is `failed`
- `Visualizar` only when the study is available in local Orthanc

### Visualizar Enablement Rule

- `Visualizar` is enabled only after local availability is confirmed
- In the current design baseline this aligns with retrieve state `done` plus cache confirmation

### Explicitly Required Context

- remote PACS location list
- local cache presence
- retrieve progress or terminal state
- partial-filter warning when a node could not apply all requested filters

### OHIF Handoff

- Portal opens OHIF for a selected `studyInstanceUID`
- The current portal UX opens the viewer in a new browser tab
- OHIF is not the primary search or workflow surface

### Backend Contract For Physician Surface

- `GET /api/physician/results`
  - with active filters, runs remote QIDO search against the configured PACS node
  - without filters, may return persisted recent queries as a fallback
- `GET /api/search/stream`
  - SSE stream for federated search results
- `POST /api/physician/retrieve`
  - current first retrieve contract from the physician panel
  - receives `username` + `study_instance_uid`
  - triggers PACS-to-PACS retrieve through Orthanc REST
- `POST /api/retrieve/jobs`
  - future generic retrieve job contract for selected `studyInstanceUID`
- `GET /api/retrieve/jobs/:id`
  - retrieve job status
- `GET /api/cache/studies/:studyInstanceUID`
  - local cache availability

### Future Security Constraint

- Backend and proxy must restrict OHIF/image access by active session and allowed `StudyInstanceUID`
- Physician access should reflect authorization context and not rely on hidden viewer UI

## Contract Status

- These contracts are the current functional baseline for future patient and physician portal-owned surfaces.
- Current landing implementation is already the public product entry surface and should communicate stable workflows even while some validation steps remain placeholder-backed.
