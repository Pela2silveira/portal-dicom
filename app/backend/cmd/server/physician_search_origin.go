package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// physicianSearchOrigin is one entry in the professional search selector.
// Several PACS nodes can share it; each study result still carries the node
// that actually returned it, so retrieve keeps using that node.
type physicianSearchOrigin struct {
	ID    string
	Label string
	Nodes []PACSNodeConfig
}

func normalizeSearchOrigin(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validateSearchOrigins(nodes []PACSNodeConfig) error {
	ids := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		id := strings.ToLower(strings.TrimSpace(node.ID))
		if id == "" {
			continue
		}
		ids[id] = struct{}{}
	}
	for _, node := range nodes {
		origin := normalizeSearchOrigin(node.SearchOrigin)
		if origin == "" {
			continue
		}
		if origin == physicianSearchSourceLocalCache {
			return fmt.Errorf("pacs node %q search_origin cannot be %q", node.ID, origin)
		}
		if _, clash := ids[origin]; clash {
			return fmt.Errorf("pacs node %q search_origin %q must not match a node id", node.ID, origin)
		}
	}
	return nil
}

// physicianSearchOrigins returns groups of nodes that share a search_origin.
// Nodes without one stay individual origins and are omitted here.
func physicianSearchOrigins(nodes []PACSNodeConfig) []physicianSearchOrigin {
	order := make([]string, 0)
	grouped := make(map[string][]PACSNodeConfig)
	for _, node := range nodes {
		origin := normalizeSearchOrigin(node.SearchOrigin)
		if origin == "" {
			continue
		}
		if _, ok := grouped[origin]; !ok {
			order = append(order, origin)
		}
		grouped[origin] = append(grouped[origin], node)
	}

	out := make([]physicianSearchOrigin, 0, len(order))
	for _, id := range order {
		members := sortPACSNodesByPriority(grouped[id])
		out = append(out, physicianSearchOrigin{
			ID:    id,
			Label: physicianSearchOriginLabel(members),
			Nodes: members,
		})
	}
	return out
}

func physicianSearchOriginLabel(nodes []PACSNodeConfig) string {
	for _, node := range sortPACSNodesByPriority(nodes) {
		if name := strings.TrimSpace(node.Name); name != "" {
			return name
		}
	}
	if len(nodes) == 0 {
		return ""
	}
	return strings.TrimSpace(nodes[0].ID)
}

func sortPACSNodesByPriority(nodes []PACSNodeConfig) []PACSNodeConfig {
	sorted := append([]PACSNodeConfig(nil), nodes...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Priority == sorted[j].Priority {
			return strings.ToLower(strings.TrimSpace(sorted[i].ID)) < strings.ToLower(strings.TrimSpace(sorted[j].ID))
		}
		return sorted[i].Priority < sorted[j].Priority
	})
	return sorted
}

// pacsNodesForSearchSource returns the nodes behind a grouped search origin.
// A plain node id is not expanded: that source still queries only that node.
func pacsNodesForSearchSource(nodes []PACSNodeConfig, source string) []PACSNodeConfig {
	source = normalizeSearchOrigin(source)
	if source == "" || source == physicianSearchSourceLocalCache {
		return nil
	}
	matched := make([]PACSNodeConfig, 0)
	for _, node := range nodes {
		if normalizeSearchOrigin(node.SearchOrigin) == source {
			matched = append(matched, node)
		}
	}
	if len(matched) == 0 {
		return nil
	}
	return sortPACSNodesByPriority(matched)
}

func (a *App) configuredNodesForPhysicianSource(source string) []PACSNodeConfig {
	if a == nil || a.externalConfig == nil {
		return nil
	}
	return pacsNodesForSearchSource(a.externalConfig.PACSNodes, source)
}

// mergePhysicianResultsByStudy keeps the first result for each study UID.
// Callers pass nodes in priority order so the preferred archive owns a study
// that exists in more than one member. Results are then ordered like a
// single-node search: newest study date first.
func mergePhysicianResultsByStudy(parts [][]PhysicianResult) []PhysicianResult {
	seen := make(map[string]struct{})
	merged := make([]PhysicianResult, 0)
	for _, part := range parts {
		for _, result := range part {
			key := strings.TrimSpace(result.StudyInstanceUID)
			if key != "" {
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
			}
			merged = append(merged, result)
		}
	}
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].StudyDate == merged[j].StudyDate {
			return merged[i].StudyInstanceUID < merged[j].StudyInstanceUID
		}
		return merged[i].StudyDate > merged[j].StudyDate
	})
	return merged
}

type physicianNodeSearchOutcome struct {
	results []PhysicianResult
	err     error
	queried bool
}

// searchPhysicianResultsFromNodes queries every member of one search origin.
// A DNI search still maps PatientID with each node's own patient_id_source.
// One member failing does not discard the others; the search fails only when
// every queried member fails.
func (a *App) searchPhysicianResultsFromNodes(ctx context.Context, physician PhysicianSummary, nodes []PACSNodeConfig, filters PhysicianSearchFilters, searchByDNI bool) ([]PhysicianResult, error) {
	nodes = sortPACSNodesByPriority(nodes)
	var ids patientSearchIdentifiers
	if searchByDNI {
		sources := make([]string, 0, len(nodes))
		for _, node := range nodes {
			sources = append(sources, node.Resolved().PatientIDSource)
		}
		ids = a.resolvePatientSearchIdentifiers(ctx, filters.DocumentNumber, patientIdentifierNeedsForSources(sources))
	}

	outcomes := make([]physicianNodeSearchOutcome, len(nodes))
	var wg sync.WaitGroup
	for i, node := range nodes {
		probe := filters
		if searchByDNI {
			probe.PatientID = effectivePatientIDForNode(node.Resolved().PatientIDSource, ids)
			if probe.PatientID == "" {
				a.log("info", "physician_search_dni_unmapped", map[string]any{
					"node":   node.ID,
					"source": node.Resolved().PatientIDSource,
					"origin": normalizeSearchOrigin(filters.Source),
				})
				continue
			}
		}
		wg.Add(1)
		go func(i int, node PACSNodeConfig, probe PhysicianSearchFilters) {
			defer wg.Done()
			results, err := a.searchPhysicianResultsFromNode(ctx, physician, node, probe)
			outcomes[i] = physicianNodeSearchOutcome{results: results, err: err, queried: true}
		}(i, node, probe)
	}
	wg.Wait()

	parts := make([][]PhysicianResult, 0, len(outcomes))
	var firstErr error
	queried := 0
	succeeded := 0
	for i, outcome := range outcomes {
		if !outcome.queried {
			continue
		}
		queried++
		if outcome.err != nil {
			a.log("warn", "physician_origin_node_failed", map[string]any{
				"origin": normalizeSearchOrigin(filters.Source),
				"node":   nodes[i].ID,
				"error":  outcome.err.Error(),
			})
			if firstErr == nil {
				firstErr = outcome.err
			}
			continue
		}
		succeeded++
		parts = append(parts, outcome.results)
	}
	if queried > 0 && succeeded == 0 {
		return nil, firstErr
	}
	return mergePhysicianResultsByStudy(parts), nil
}
