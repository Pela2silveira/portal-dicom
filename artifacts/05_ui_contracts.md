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
- The viewer root `/ohif/` is not a supported navigation target for patient or physician users.
- Hiding the native OHIF study list is a UX choice and is not a security control.
- Real access control, when implemented, must be enforced in the backend and image proxy by active portal session and allowed `StudyInstanceUID`.
- All portal-owned surfaces must be responsive and usable on mobile for consultation workflows.

## Patient Contract

### Purpose

Allow a patient to see only their authorized studies and open one selected study in OHIF.

### Entry Surface

- Public landing flow: `Documento + código por mail`
- The `Documento` field must accept digits only, sanitize non-numeric input in the browser, and reject implausible lengths before calling backend routes.
- The `Enviar código` action must call backend prevalidation before any future mail delivery integration.
- The `Enviar código` button must stay visually associated with the `Documento` input, not with the mail-code input.
- The `Continuar` action should use the same primary blue CTA language and must stay disabled until the mail-code request succeeds and the patient enters a code value.
- Required patient outcomes:
  - `ready_to_send`: proceed with mail-code UX
  - `missing_active_email`: show contact-update guidance in a prominent warning style
  - `patient_not_found`: show that the patient has no records
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
- Institutional logos such as ANDES and RedTICS may appear in the footer as secondary brand references and should remain non-interactive.
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
- `birthDate` (label corto visible: `F. nacimiento`)
- `sex`
- `genderIdentity` (label corto visible: `Genero`)

### Patient Summary Layout

- `fullName` should occupy twice the horizontal width of each other demographic field whenever the available width allows a multi-column layout
- `documentNumber`, `birthDate`, `sex`, and `genderIdentity` should remain proportionally narrower than `fullName` in desktop layouts while keeping responsive collapse behavior
- the patient summary grid should collapse fluidly as available width shrinks, including browser zoom scenarios

### Allowed Filters

- `period`
  values: `today`, `week`, `month`, `year`, `custom`, or empty for all dates
  behavior: the dropdown presets must set the patient date range automatically
  default: `month`
  visual rule: the visible preset selector must always match the active internal date range
- `dateRange`
  behavior: a calendar range picker inside an inline dropdown allows first click = start date and second click = end date, with the selected interval highlighted
  contract: portal-facing filtering uses normalized ISO dates (`YYYY-MM-DD`) even if upstream DICOM metadata arrives as `YYYYMMDD`
  layout: the date preset selector and range dropdown should sit horizontally within the shared date filter block when space allows
  visual: the shared date filter block should use a transparent background with a thin outline, while the calendar popup remains contained relative to the range control
  interaction: the calendar dropdown should close automatically when the user clicks outside the date filter block
  single-day rule: if the user selects only one day, the search should use that same date as both start and end
  same-day rule: if the user clicks the same date again while choosing the end of a range, the picker should collapse back to a single-day selection
  state rule: after the first click the picker enters an intermediate "awaiting end date" state and stays open until the user chooses the end date or clicks outside
  copy rule: the visible summary should stay concise and should not explain the interaction mechanics
- `modality`
  values: `all` or one enumerated public-facing modality at a time, shown to the patient with plain-language labels in Spanish
  visual: the modality selector should use the same transparent grouped container language as the date filter block
  label: `Tipo de Estudio`
  option format: `Nombre descriptivo (SIGLA)`
- free text is out of scope for the first patient surface

### Patient Filter Layout

- The patient filter row should align three elements horizontally when space allows:
  `Fecha`, `Tipo de Estudio`, and `Buscar`
- The search button should stay visually aligned with the filter controls and should not wrap into a competing second row on desktop
- The filter layout should reflow before controls overlap, including under browser zoom

### Allowed Sort

- default sort: newest `studyDate` first
- grouping: first by primary modality, then by newest `studyDate` inside each group

### Availability States

- `available_local`: study is already in local Orthanc and can be opened now
- `pending_retrieve`: study is expected but not yet available for viewing; display label `Recuperacion pendiente`
- `unavailable`: study exists in patient authorization scope but is not currently retrievable
- `error`: retrieval or authorization resolution failed

### Allowed Actions

- `Recuperar estudio` when `availabilityStatus = pending_retrieve`
- once a retrieve is `queued` or `running`, the patient action must render as disabled `Recuperando` and must not enqueue duplicate jobs for the same study
- `Ver estudio` when `availabilityStatus = available_local`
- `Buscar` to call `POST /api/patient/search` with the current patient filters while keeping cached results visible
- the patient result area must differentiate QIDO search feedback from per-study retrieve state without adding parallel UI state machines

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
  - includes sync state for the current filter set (`idle|queued|running|done|failed`)
  - includes per-study `retrieve_status` resolved from `retrieve_jobs`
- `POST /api/patient/search`
  - receives `document_number` plus the current patient filters
  - enqueues background QIDO work and returns `request_id`
- `GET /api/patient/search?request_id=...`
  - returns the current worker status for the patient search
- `POST /api/patient/retrieve`
  - receives `document_number` + `study_instance_uid`
  - enqueues a background retrieve job that triggers PACS-to-PACS retrieve through Orthanc REST
  - returns `job_id` and the UI follows completion through `GET /api/retrieve/jobs/:id/events` (SSE)
  - the patient list should refresh on retrieve terminal events (`done|failed`), not through unbounded list polling
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
- Institutional logos such as ANDES and RedTICS may appear in the footer as secondary brand references and should remain non-interactive.
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
- while `retrieve_status` is `queued|running`, the action must stay disabled and the backend should reuse the active job instead of enqueuing duplicates
- `Visualizar` only when the study is available in local Orthanc

### Visualizar Enablement Rule

- `Visualizar` is enabled only after local availability is confirmed
- In the current design baseline this aligns with retrieve state `done` plus cache confirmation

### Explicitly Required Context

- remote PACS location list
- local cache presence
- retrieve progress or terminal state
- partial-filter warning when a node could not apply all requested filters
- retrieve progress must be readable directly in the result row and action state

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
  - triggers PACS-to-PACS retrieve through Orthanc REST and returns `job_id`
- `GET /api/retrieve/jobs/:id/events`
  - SSE stream for retrieve lifecycle events (`running`, `done`, `failed`)
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
