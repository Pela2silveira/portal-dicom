package main

import (
	"net/url"
	"strings"
)

// retrieveAETDecision is the outcome of classifying a remote study by
// Retrieve AE Title (0008,0054) against the node that was queried.
type retrieveAETDecision struct {
	Keep         bool
	RetrieveAETs []string
	Reason       string
}

// dicomwebHostKey normalizes a DICOMweb base URL to scheme://host so nodes that
// share a multi-AET archive (same host, different /aets/{AE}/ path) can be
// detected as a cluster.
func dicomwebHostKey(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || strings.TrimSpace(parsed.Host) == "" {
		return ""
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme == "" {
		scheme = "https"
	}
	return scheme + "://" + strings.ToLower(parsed.Host)
}

// nodeDICOMwebHostKey returns the host key for a configured PACS node.
func nodeDICOMwebHostKey(node PACSNodeConfig) string {
	resolved := node.Resolved()
	return dicomwebHostKey(resolved.DICOMwebBaseURL)
}

// nodeExpectedRetrieveAET is the AE Title this portal node claims for retrieve
// (and therefore for RetrieveAETitle ownership on a shared archive).
func nodeExpectedRetrieveAET(node PACSNodeConfig) string {
	return strings.TrimSpace(node.Resolved().AET)
}

// retrieveAETitlesFromItem reads Retrieve AE Title (0008,0054) from a QIDO/C-FIND item.
func retrieveAETitlesFromItem(item qidoResponseItem) []string {
	values := dicomStringList(item, "00080054")
	if len(values) > 0 {
		return values
	}
	if single := dicomFirstString(item, "00080054"); single != "" {
		return []string{single}
	}
	return nil
}

func retrieveAETListContains(retrieveAETs []string, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return false
	}
	for _, aet := range retrieveAETs {
		if strings.EqualFold(strings.TrimSpace(aet), expected) {
			return true
		}
	}
	return false
}

// classifyStudyByRetrieveAET keeps a study only when its Retrieve AE Title
// matches the queried node's configured AET. Used for multi-AET archives where
// /aets/{AE}/rs returns the whole shared catalog.
func classifyStudyByRetrieveAET(enabled bool, expectedAET string, retrieveAETs []string) retrieveAETDecision {
	if !enabled {
		return retrieveAETDecision{Keep: true, RetrieveAETs: retrieveAETs, Reason: "disabled"}
	}
	expectedAET = strings.TrimSpace(expectedAET)
	if expectedAET == "" {
		// Cannot classify without a configured AET; keep to avoid wiping results.
		return retrieveAETDecision{Keep: true, RetrieveAETs: retrieveAETs, Reason: "no_expected_aet"}
	}
	if len(retrieveAETs) == 0 {
		return retrieveAETDecision{Keep: false, RetrieveAETs: nil, Reason: "empty_retrieve_aet"}
	}
	if retrieveAETListContains(retrieveAETs, expectedAET) {
		return retrieveAETDecision{Keep: true, RetrieveAETs: retrieveAETs, Reason: "matched"}
	}
	return retrieveAETDecision{Keep: false, RetrieveAETs: retrieveAETs, Reason: "mismatched_retrieve_aet"}
}

// shouldClassifyByRetrieveAET decides whether a node must post-filter QIDO/C-FIND
// results by Retrieve AE Title.
//
// Precedence:
//  1. Explicit search.classify_by_retrieve_aet when set
//  2. Auto-on when another configured node shares the same DICOMweb host
func (a *App) shouldClassifyByRetrieveAET(node PACSNodeConfig) bool {
	if node.Search.ClassifyByRetrieveAET != nil {
		return *node.Search.ClassifyByRetrieveAET
	}
	return a.nodeSharesDICOMwebHost(node)
}

func (a *App) nodeSharesDICOMwebHost(node PACSNodeConfig) bool {
	if a == nil || a.externalConfig == nil {
		return false
	}
	hostKey := nodeDICOMwebHostKey(node)
	if hostKey == "" {
		return false
	}
	for _, other := range a.externalConfig.PACSNodes {
		if strings.EqualFold(strings.TrimSpace(other.ID), strings.TrimSpace(node.ID)) {
			continue
		}
		if nodeDICOMwebHostKey(other) == hostKey {
			return true
		}
	}
	return false
}
