package main

import (
	"context"
	"sort"
	"strings"
)

// A node's patient_id_source declares which patient identifier travels as the
// DICOM PatientID (0010,0020) when the professional searches by DNI. PACS nodes
// are queried by DICOM PatientID; the identifier that value maps to differs per
// node (the id<->dni correspondence is owned by the HIS), so each node states
// its rule. Direct searches by ID bypass this entirely.
const (
	patientIDSourceDNI       = "dni"
	patientIDSourceMongoID   = "mongo_id"
	patientIDSourceLegacyHIS = "legacy_his"
)

// patientSearchIdentifiers holds the identifiers known for a patient resolved
// from a DNI. Fields are populated lazily, only when a node's mapping rule needs
// them.
type patientSearchIdentifiers struct {
	DocumentNumber string
	MongoID        string
	LegacyHISCode  string
}

// patientIdentifierNeeds declares which (impure, external) identifier lookups a
// resolution should perform, so we only hit Mongo/HIS/MSSQL when a configured
// node's mapping rule actually requires it.
type patientIdentifierNeeds struct {
	Mongo     bool
	LegacyHIS bool
}

// normalizePatientIDSource canonicalizes a node's patient_id_source rule.
// Empty defaults to "dni". Unknown values are returned trimmed/lowercased so a
// future HIS-provider field can be added without silently mapping to DNI.
func normalizePatientIDSource(source string) string {
	switch s := strings.ToLower(strings.TrimSpace(source)); s {
	case "", "dni", "document_number", "documento":
		return patientIDSourceDNI
	case "mongo_id", "mongoid", "mongo_object_id":
		return patientIDSourceMongoID
	case "legacy_his", "legacy", "mssql_codigo", "codigo":
		return patientIDSourceLegacyHIS
	default:
		return s
	}
}

// patientIDSourceNeedsMongo reports whether resolving this source requires the
// patient's Mongo id (so we only hit the HIS when actually needed).
func patientIDSourceNeedsMongo(source string) bool {
	return normalizePatientIDSource(source) == patientIDSourceMongoID
}

// patientIDSourceNeedsLegacyHIS reports whether resolving this source requires a
// lookup against the legacy HIS (MSSQL) to map DNI -> internal patient code.
func patientIDSourceNeedsLegacyHIS(source string) bool {
	return normalizePatientIDSource(source) == patientIDSourceLegacyHIS
}

// patientIdentifierNeedsForSources aggregates the external lookups required by a
// set of node mapping rules.
func patientIdentifierNeedsForSources(sources []string) patientIdentifierNeeds {
	var needs patientIdentifierNeeds
	for _, source := range sources {
		if patientIDSourceNeedsMongo(source) {
			needs.Mongo = true
		}
		if patientIDSourceNeedsLegacyHIS(source) {
			needs.LegacyHIS = true
		}
	}
	return needs
}

// effectivePatientIDForNode returns the DICOM PatientID value to send to a node
// for a DNI-based search, per the node's rule. An empty result means the node
// cannot be queried for this patient (the required identifier is unknown, or the
// rule is a not-yet-implemented provider field).
func effectivePatientIDForNode(source string, ids patientSearchIdentifiers) string {
	switch normalizePatientIDSource(source) {
	case patientIDSourceDNI:
		return strings.TrimSpace(ids.DocumentNumber)
	case patientIDSourceMongoID:
		return strings.TrimSpace(ids.MongoID)
	case patientIDSourceLegacyHIS:
		return strings.TrimSpace(ids.LegacyHISCode)
	default:
		return ""
	}
}

// candidateLocalCachePatientIDs returns the distinct, non-empty DICOM PatientID
// values that locally cached studies could carry for this patient, given the set
// of patient_id_source rules across configured nodes. The local Orthanc holds a
// union of studies retrieved from those nodes, each keyed by its node's mapped
// PatientID, so a DNI search must probe every distinct mapped value.
func candidateLocalCachePatientIDs(ids patientSearchIdentifiers, sources []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(sources))
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(sources) == 0 {
		sources = []string{patientIDSourceDNI}
	}
	for _, source := range sources {
		add(effectivePatientIDForNode(source, ids))
	}
	sort.Strings(out)
	return out
}

// distinctConfiguredPatientIDSources returns the set of patient_id_source rules
// across configured PACS nodes (normalized), defaulting to {"dni"}.
func (a *App) distinctConfiguredPatientIDSources() []string {
	if a.externalConfig == nil {
		return []string{patientIDSourceDNI}
	}
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, node := range a.externalConfig.PACSNodes {
		source := normalizePatientIDSource(node.PatientIDSource)
		if _, ok := seen[source]; ok {
			continue
		}
		seen[source] = struct{}{}
		out = append(out, source)
	}
	if len(out) == 0 {
		return []string{patientIDSourceDNI}
	}
	return out
}

// resolvePatientSearchIdentifiers resolves a DNI into the identifiers usable for
// PACS queries. It reads the local patient_identifiers cache first (populated by
// the patient portal login flow) and, only when the Mongo id is required and not
// cached, resolves it live from the HIS/Mongo identity source, which also
// refreshes the cache. Resolution failures are non-fatal: callers degrade to the
// identifiers available (at least the DNI itself).
func (a *App) resolvePatientSearchIdentifiers(ctx context.Context, documentNumber string, needs patientIdentifierNeeds) patientSearchIdentifiers {
	ids := patientSearchIdentifiers{DocumentNumber: normalizeDocumentNumberCandidate(documentNumber)}
	if ids.DocumentNumber == "" {
		return ids
	}

	if needs.Mongo {
		if cached, err := a.loadCachedMongoObjectIDByDocument(ctx, ids.DocumentNumber); err == nil {
			ids.MongoID = cached
		} else {
			a.log("warn", "physician_search_identifier_cache_failed", map[string]any{
				"error": err.Error(),
			})
		}

		if ids.MongoID == "" {
			if _, identity, err := a.ensurePatientRecordWithIdentity(ctx, ids.DocumentNumber); err == nil {
				ids.MongoID = mongoObjectIDFromAlternateIdentifiers(identity.AlternateIDs)
			} else {
				a.log("warn", "physician_search_identifier_resolve_failed", map[string]any{
					"error": err.Error(),
				})
			}
		}
	}

	if needs.LegacyHIS {
		ids.LegacyHISCode = a.resolveLegacyHISCode(ctx, ids.DocumentNumber)
	}

	return ids
}

// resolveLegacyHISCode maps a DNI to the legacy HIS patient code. It reads the
// patient_identifiers cache first and, on a miss, queries the legacy HIS (MSSQL)
// live and best-effort persists the mapping. Failures are non-fatal: an empty
// result means the DNI could not be mapped and the node is skipped.
func (a *App) resolveLegacyHISCode(ctx context.Context, documentNumber string) string {
	if cached, err := a.loadCachedLegacyHISCodeByDocument(ctx, documentNumber); err == nil {
		if cached != "" {
			return cached
		}
	} else {
		a.log("warn", "legacy_his_cache_failed", map[string]any{
			"error": err.Error(),
		})
	}

	if a.legacyHIS == nil {
		a.log("warn", "legacy_his_not_configured", map[string]any{
			"hint": "legacy_his disabled or client failed to init; check config.legacy_his.enabled/host and LEGACY_HIS_PASSWORD (see startup_dependency_unavailable at boot)",
		})
		return ""
	}

	code, err := a.legacyHIS.LookupCodigoByDocument(ctx, documentNumber)
	if err != nil {
		a.log("warn", "legacy_his_lookup_failed", map[string]any{
			"error": err.Error(),
		})
		return ""
	}
	if code == "" {
		a.log("info", "legacy_his_no_match", map[string]any{})
		return ""
	}

	if err := a.persistLegacyHISCode(ctx, documentNumber, code); err != nil {
		a.log("warn", "legacy_his_persist_failed", map[string]any{
			"error": err.Error(),
		})
	}
	return code
}
