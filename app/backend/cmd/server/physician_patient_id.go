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
	patientIDSourceDNI     = "dni"
	patientIDSourceMongoID = "mongo_id"
)

// patientSearchIdentifiers holds the identifiers known for a patient resolved
// from a DNI. It is intentionally small today (DNI + Mongo id); other HIS
// provider fields will extend it later.
type patientSearchIdentifiers struct {
	DocumentNumber string
	MongoID        string
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
	default:
		return s
	}
}

// patientIDSourceNeedsMongo reports whether resolving this source requires the
// patient's Mongo id (so we only hit the HIS when actually needed).
func patientIDSourceNeedsMongo(source string) bool {
	return normalizePatientIDSource(source) == patientIDSourceMongoID
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
func (a *App) resolvePatientSearchIdentifiers(ctx context.Context, documentNumber string, needMongo bool) patientSearchIdentifiers {
	ids := patientSearchIdentifiers{DocumentNumber: normalizeDocumentNumberCandidate(documentNumber)}
	if ids.DocumentNumber == "" {
		return ids
	}

	if cached, err := a.loadCachedMongoObjectIDByDocument(ctx, ids.DocumentNumber); err == nil {
		ids.MongoID = cached
	} else {
		a.log("warn", "physician_search_identifier_cache_failed", map[string]any{
			"error": err.Error(),
		})
	}

	if needMongo && ids.MongoID == "" {
		if _, identity, err := a.ensurePatientRecordWithIdentity(ctx, ids.DocumentNumber); err == nil {
			ids.MongoID = mongoObjectIDFromAlternateIdentifiers(identity.AlternateIDs)
		} else {
			a.log("warn", "physician_search_identifier_resolve_failed", map[string]any{
				"error": err.Error(),
			})
		}
	}

	return ids
}
