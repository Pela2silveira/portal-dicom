package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type physicianAndesEnrichJob struct {
	PhysicianID string
	NodeID      string
	Filters     PhysicianSearchFilters
	Results     []PhysicianResult
}

type PrestacionLookupSource interface {
	ProviderName() string
	Mode() string
	FindByStudyUIDs(ctx context.Context, studyUIDs []string) (map[string]AndesPrestacionSummary, error)
	FindByPatientMongoID(ctx context.Context, mongoID string, conceptIDs []string) (map[string]AndesPrestacionSummary, error)
}

type AndesPrestacionSummary struct {
	PrestacionID  string `json:"prestacion_id,omitempty"`
	PrestacionFSN string `json:"prestacion_fsn,omitempty"`
	Professional  string `json:"professional,omitempty"`
}

type MongoPrestacionLookupSource struct {
	client         *mongo.Client
	collection     *mongo.Collection
	connectTimeout time.Duration
	queryTimeout   time.Duration
	batchSize      int
}

type NoopPrestacionLookupSource struct{}

type MongoPrestacionMetadataEntry struct {
	Key   string `bson:"key"`
	Valor any    `bson:"valor"`
}

type MongoPrestacionLookupDocument struct {
	ID        primitive.ObjectID             `bson:"_id"`
	Metadata  []MongoPrestacionMetadataEntry `bson:"metadata"`
	Solicitud struct {
		TipoPrestacion struct {
			FSN string `bson:"fsn"`
		} `bson:"tipoPrestacion"`
		Profesional struct {
			Nombre   string `bson:"nombre"`
			Apellido string `bson:"apellido"`
		} `bson:"profesional"`
	} `bson:"solicitud"`
}

func (s *MongoPrestacionLookupSource) ProviderName() string {
	return "his_mongo_direct"
}

func (s *MongoPrestacionLookupSource) Mode() string {
	return HISPrestacionesProviderMongo
}

func (s *MongoPrestacionLookupSource) FindByPatientMongoID(_ context.Context, _ string, _ []string) (map[string]AndesPrestacionSummary, error) {
	return map[string]AndesPrestacionSummary{}, nil
}

func (s *MongoPrestacionLookupSource) Close(ctx context.Context) error {
	disconnectCtx, cancel := context.WithTimeout(ctx, s.connectTimeout)
	defer cancel()
	return s.client.Disconnect(disconnectCtx)
}

func (s *NoopPrestacionLookupSource) ProviderName() string {
	return "noop"
}

func (s *NoopPrestacionLookupSource) FindByStudyUIDs(_ context.Context, _ []string) (map[string]AndesPrestacionSummary, error) {
	return map[string]AndesPrestacionSummary{}, nil
}

func (s *NoopPrestacionLookupSource) Mode() string {
	return "noop"
}

func (s *NoopPrestacionLookupSource) FindByPatientMongoID(_ context.Context, _ string, _ []string) (map[string]AndesPrestacionSummary, error) {
	return map[string]AndesPrestacionSummary{}, nil
}

func extractPACSSUIDFromPrestacionMetadata(entries []MongoPrestacionMetadataEntry) string {
	for _, entry := range entries {
		if !strings.EqualFold(strings.TrimSpace(entry.Key), "pacs-uid") {
			continue
		}
		return strings.TrimSpace(fmt.Sprint(entry.Valor))
	}
	return ""
}

func andesPrestacionSummaryFromMongo(doc MongoPrestacionLookupDocument) AndesPrestacionSummary {
	professional := strings.TrimSpace(strings.TrimSpace(doc.Solicitud.Profesional.Apellido) + ", " + strings.TrimSpace(doc.Solicitud.Profesional.Nombre))
	if strings.TrimSpace(doc.Solicitud.Profesional.Apellido) == "" {
		professional = strings.TrimSpace(doc.Solicitud.Profesional.Nombre)
	}
	return AndesPrestacionSummary{
		PrestacionID:  doc.ID.Hex(),
		PrestacionFSN: strings.TrimSpace(doc.Solicitud.TipoPrestacion.FSN),
		Professional:  professional,
	}
}

func (s *MongoPrestacionLookupSource) findByFilter(ctx context.Context, filter bson.M) (map[string]AndesPrestacionSummary, error) {
	queryCtx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	cursor, err := s.collection.Find(queryCtx, filter, options.Find().SetProjection(bson.M{
		"_id":                            1,
		"metadata":                       1,
		"solicitud.tipoPrestacion.fsn":   1,
		"solicitud.profesional.nombre":   1,
		"solicitud.profesional.apellido": 1,
	}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(queryCtx)

	var docs []MongoPrestacionLookupDocument
	if err := cursor.All(queryCtx, &docs); err != nil {
		return nil, err
	}

	results := make(map[string]AndesPrestacionSummary, len(docs))
	for _, doc := range docs {
		studyUID := extractPACSSUIDFromPrestacionMetadata(doc.Metadata)
		if studyUID == "" {
			continue
		}
		if _, exists := results[studyUID]; exists {
			continue
		}
		results[studyUID] = andesPrestacionSummaryFromMongo(doc)
	}
	return results, nil
}

func (s *MongoPrestacionLookupSource) FindByStudyUIDs(ctx context.Context, studyUIDs []string) (map[string]AndesPrestacionSummary, error) {
	if len(studyUIDs) == 0 {
		return map[string]AndesPrestacionSummary{}, nil
	}

	results := make(map[string]AndesPrestacionSummary, len(studyUIDs))
	for _, batch := range chunkStrings(studyUIDs, s.batchSize) {
		batchResults, err := s.findByFilter(ctx, bson.M{
			"metadata": bson.M{
				"$elemMatch": bson.M{
					"key":   "pacs-uid",
					"valor": bson.M{"$in": batch},
				},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("lookup prestaciones by pacs-uid batch_size=%d: %w", len(batch), err)
		}
		for studyUID, summary := range batchResults {
			if _, exists := results[studyUID]; exists {
				continue
			}
			results[studyUID] = summary
		}
	}

	return results, nil
}

type AndesRESTPrestacionLookupSource struct {
	httpClient *http.Client
	baseURL    string
	token      string
	timeout    time.Duration
}

type andesRESTPrestacionMetadataEntry struct {
	Key   string          `json:"key"`
	Valor json.RawMessage `json:"valor"`
}

type andesRESTPrestacionDoc struct {
	ID       string                             `json:"id"`
	Metadata []andesRESTPrestacionMetadataEntry `json:"metadata"`
	Paciente struct {
		ID string `json:"id"`
	} `json:"paciente"`
	Solicitud struct {
		TipoPrestacion struct {
			ConceptID string `json:"conceptId"`
			FSN       string `json:"fsn"`
			Term      string `json:"term"`
		} `json:"tipoPrestacion"`
		Profesional struct {
			Nombre   string `json:"nombre"`
			Apellido string `json:"apellido"`
		} `json:"profesional"`
	} `json:"solicitud"`
}

func (s *AndesRESTPrestacionLookupSource) ProviderName() string {
	return "andes_rest"
}

func (s *AndesRESTPrestacionLookupSource) Mode() string {
	return HISPrestacionesProviderREST
}

func (s *AndesRESTPrestacionLookupSource) Healthy() bool {
	return strings.TrimSpace(s.token) != "" && strings.TrimSpace(s.baseURL) != ""
}

func (s *AndesRESTPrestacionLookupSource) FindByStudyUIDs(_ context.Context, _ []string) (map[string]AndesPrestacionSummary, error) {
	return map[string]AndesPrestacionSummary{}, nil
}

func (s *AndesRESTPrestacionLookupSource) FindByPatientMongoID(ctx context.Context, mongoID string, conceptIDs []string) (map[string]AndesPrestacionSummary, error) {
	mongoID = strings.TrimSpace(mongoID)
	if mongoID == "" {
		return map[string]AndesPrestacionSummary{}, nil
	}
	if strings.TrimSpace(s.token) == "" {
		return nil, errors.New("andes rest: missing HIS_TOKEN")
	}

	endpoint := strings.TrimRight(strings.TrimSpace(s.baseURL), "/") + "/modules/rup/prestaciones"
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("andes rest: invalid base url: %w", err)
	}
	q := parsed.Query()
	q.Set("idPaciente", mongoID)
	q.Set("estado", "validada")
	for _, c := range conceptIDs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		q.Add("tipoPrestaciones", c)
	}
	parsed.RawQuery = q.Encode()

	timeout := s.timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("andes rest: build request: %w", err)
	}
	req.Header.Set("Authorization", "JWT "+s.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "identity")

	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("andes rest: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("andes rest: 401 unauthorized")
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("andes rest: http %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var docs []andesRESTPrestacionDoc
	if err := json.NewDecoder(resp.Body).Decode(&docs); err != nil {
		return nil, fmt.Errorf("andes rest: decode body: %w", err)
	}

	results := make(map[string]AndesPrestacionSummary, len(docs))
	for _, doc := range docs {
		if strings.TrimSpace(doc.Paciente.ID) != "" && !strings.EqualFold(strings.TrimSpace(doc.Paciente.ID), mongoID) {
			continue
		}
		pacsUID := extractPACSSUIDFromRESTMetadata(doc.Metadata)
		if pacsUID == "" {
			continue
		}
		if _, exists := results[pacsUID]; exists {
			continue
		}
		fsn := strings.TrimSpace(doc.Solicitud.TipoPrestacion.FSN)
		if fsn == "" {
			fsn = strings.TrimSpace(doc.Solicitud.TipoPrestacion.Term)
		}
		results[pacsUID] = AndesPrestacionSummary{
			PrestacionID:  strings.TrimSpace(doc.ID),
			PrestacionFSN: fsn,
			Professional:  joinProfessionalName(doc.Solicitud.Profesional.Apellido, doc.Solicitud.Profesional.Nombre),
		}
	}
	return results, nil
}

type CompositePrestacionLookupSource struct {
	rest  *AndesRESTPrestacionLookupSource
	mongo *MongoPrestacionLookupSource
	mode  string
}

func (c *CompositePrestacionLookupSource) ProviderName() string {
	parts := make([]string, 0, 2)
	if c.rest != nil {
		parts = append(parts, "rest")
	}
	if c.mongo != nil {
		parts = append(parts, "mongo")
	}
	if len(parts) == 0 {
		return "noop"
	}
	return fmt.Sprintf("composite[%s,mode=%s]", strings.Join(parts, "+"), c.mode)
}

func (c *CompositePrestacionLookupSource) Mode() string {
	return c.mode
}

func (c *CompositePrestacionLookupSource) FindByStudyUIDs(ctx context.Context, studyUIDs []string) (map[string]AndesPrestacionSummary, error) {
	if c.mongo == nil {
		return map[string]AndesPrestacionSummary{}, nil
	}
	return c.mongo.FindByStudyUIDs(ctx, studyUIDs)
}

func (c *CompositePrestacionLookupSource) FindByPatientMongoID(ctx context.Context, mongoID string, conceptIDs []string) (map[string]AndesPrestacionSummary, error) {
	if c.rest == nil {
		return map[string]AndesPrestacionSummary{}, nil
	}
	return c.rest.FindByPatientMongoID(ctx, mongoID, conceptIDs)
}

func (c *CompositePrestacionLookupSource) Close(ctx context.Context) error {
	if c.mongo != nil {
		_ = c.mongo.Close(ctx)
	}
	return nil
}

func studyUIDLikelyAndesIssued(studyUID string, prefixes []string) bool {
	studyUID = strings.TrimSpace(studyUID)
	if studyUID == "" {
		return false
	}
	if len(prefixes) == 0 {
		return true
	}
	for _, prefix := range prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			continue
		}
		if strings.HasPrefix(studyUID, prefix) {
			return true
		}
	}
	return false
}

const (
	HISPrestacionesProviderMongo = "mongo"
	HISPrestacionesProviderREST  = "rest"
	HISPrestacionesProviderAuto  = "auto"
)

var defaultAndesUIDPrefixes = []string{"2.16.840.1.113883.2.10.35.1.200."}

func (c HISConfig) ResolvedPrestacionesProvider() string {
	mode := strings.ToLower(strings.TrimSpace(c.PrestacionesProvider))
	switch mode {
	case HISPrestacionesProviderMongo, HISPrestacionesProviderREST, HISPrestacionesProviderAuto:
		return mode
	default:
		return HISPrestacionesProviderREST
	}
}

func (c HISConfig) ResolvedAndesUIDPrefixes() []string {
	prefixes := make([]string, 0, len(c.AndesUIDPrefixes))
	for _, raw := range c.AndesUIDPrefixes {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		prefixes = append(prefixes, trimmed)
	}
	if len(prefixes) == 0 {
		return append([]string(nil), defaultAndesUIDPrefixes...)
	}
	return prefixes
}

func (c HISConfig) ResolvedAndesRESTConcurrency() int {
	if c.AndesRESTConcurrency > 0 {
		return c.AndesRESTConcurrency
	}
	return 2
}

func (c HISConfig) ResolvedAndesRESTRequestTimeout() time.Duration {
	if c.AndesRESTRequestTimeoutMS > 0 {
		return time.Duration(c.AndesRESTRequestTimeoutMS) * time.Millisecond
	}
	return 3 * time.Second
}

type PACSTipoPrestacionConfig struct {
	ConceptID   string `json:"conceptId,omitempty"`
	Term        string `json:"term,omitempty"`
	Nombre      string `json:"nombre,omitempty"`
	FSN         string `json:"fsn,omitempty"`
	SemanticTag string `json:"semanticTag,omitempty"`
}

func (a *App) andesEnrichWorkerConcurrency() int {
	if a.externalConfig == nil {
		return 1
	}
	concurrency := a.externalConfig.HIS.ResolvedAndesRESTConcurrency()
	if concurrency <= 0 {
		return 1
	}
	return concurrency
}

func (a *App) startPhysicianAndesEnrichWorker() {
	workers := a.andesEnrichWorkerConcurrency()
	for workerIndex := 0; workerIndex < workers; workerIndex++ {
		go func() {
			for job := range a.physicianAndesEnrichQueue {
				a.processPhysicianAndesEnrichJob(job)
			}
		}()
	}
}

func (a *App) enqueuePhysicianAndesEnrichJob(physician PhysicianSummary, node PACSNodeConfig, filters PhysicianSearchFilters, results []PhysicianResult) {
	if len(results) == 0 {
		return
	}
	job := physicianAndesEnrichJob{
		PhysicianID: physician.ID,
		NodeID:      node.ID,
		Filters:     filters,
		Results:     clonePhysicianResults(results),
	}
	select {
	case a.physicianAndesEnrichQueue <- job:
		a.log("info", "physician_andes_enrichment_queued", map[string]any{
			"physician_id": physician.ID,
			"node_id":      node.ID,
			"result_count": len(results),
		})
	default:
		a.log("warn", "physician_andes_enrichment_queue_full", map[string]any{
			"physician_id": physician.ID,
			"node_id":      node.ID,
			"result_count": len(results),
		})
	}
}

func (a *App) processPhysicianAndesEnrichJob(job physicianAndesEnrichJob) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := a.enrichPhysicianResultsWithAndes(ctx, job.Results); err != nil {
		a.log("error", "physician_andes_enrichment_failed", map[string]any{
			"physician_id": job.PhysicianID,
			"node_id":      job.NodeID,
			"error":        err.Error(),
		})
	}
	if err := a.persistPhysicianResultsToQIDOCache(ctx, job.Results); err != nil {
		a.log("error", "physician_qido_cache_persist_failed", map[string]any{
			"physician_id": job.PhysicianID,
			"node_id":      job.NodeID,
			"error":        err.Error(),
		})
	}

	if err := a.persistPhysicianRecentQuery(ctx, job.PhysicianID, job.Filters, job.Results); err != nil {
		a.log("error", "physician_recent_query_persist_failed", map[string]any{
			"physician_id": job.PhysicianID,
			"node_id":      job.NodeID,
			"error":        err.Error(),
		})
	}
}

func (a *App) handlePatientAndesReportDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	documentNumber := strings.TrimSpace(r.URL.Query().Get("document"))
	studyInstanceUID := strings.TrimSpace(r.URL.Query().Get("study_instance_uid"))
	if studyInstanceUID == "" {
		http.Error(w, "missing required query params", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	_, patient, err := a.requirePatientSessionSummary(ctx, r)
	if err != nil {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}
	if documentNumber != "" {
		if err := validateDocumentNumber(documentNumber); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if subtle.ConstantTimeCompare([]byte(documentNumber), []byte(patient.DocumentNumber)) != 1 {
			http.Error(w, "patient session does not match requested document", http.StatusForbidden)
			return
		}
	}

	authorized, err := a.patientStudyAccessible(ctx, patient.ID, studyInstanceUID)
	if err != nil {
		a.log("error", "patient_andes_report_authorization_failed", map[string]any{
			"patient_id":         patient.ID,
			"study_instance_uid": studyInstanceUID,
			"error":              err.Error(),
		})
		http.Error(w, "failed to validate patient study", http.StatusInternalServerError)
		return
	}
	if !authorized {
		http.Error(w, "study not available for patient report", http.StatusNotFound)
		return
	}

	prestacionID, err := a.loadAndesPrestacionIDByStudyUID(ctx, studyInstanceUID)
	if err != nil {
		a.log("error", "patient_andes_report_lookup_failed", map[string]any{
			"patient_id":         patient.ID,
			"study_instance_uid": studyInstanceUID,
			"error":              err.Error(),
		})
		http.Error(w, "failed to resolve andes report metadata", http.StatusInternalServerError)
		return
	}
	if prestacionID == "" {
		http.Error(w, "study has no associated andes prestacion", http.StatusNotFound)
		return
	}

	reportPDF, err := a.downloadAndesPrestacionReportPDF(ctx, prestacionID)
	if err != nil {
		a.log("error", "patient_andes_report_download_failed", map[string]any{
			"patient_id":          patient.ID,
			"study_instance_uid":  studyInstanceUID,
			"andes_prestacion_id": prestacionID,
			"error":               err.Error(),
		})
		http.Error(w, "failed to download andes report", http.StatusBadGateway)
		return
	}

	filename := "Informe-" + sanitizeFilename(studyInstanceUID) + ".pdf"
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(reportPDF)))
	if _, err := w.Write(reportPDF); err != nil {
		a.log("warn", "patient_andes_report_write_failed", map[string]any{
			"patient_id":          patient.ID,
			"study_instance_uid":  studyInstanceUID,
			"andes_prestacion_id": prestacionID,
			"error":               err.Error(),
		})
		return
	}

	a.log("info", "patient_andes_report_download_completed", map[string]any{
		"patient_id":          patient.ID,
		"study_instance_uid":  studyInstanceUID,
		"andes_prestacion_id": prestacionID,
		"size_bytes":          len(reportPDF),
	})
}

func (a *App) handlePhysicianAndesReportDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := normalizeProfessionalDocumentInput(r.URL.Query().Get("username"))
	studyInstanceUID := strings.TrimSpace(r.URL.Query().Get("study_instance_uid"))
	if studyInstanceUID == "" {
		http.Error(w, "missing required query params", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	_, physician, err := a.requirePhysicianSessionSummary(ctx, r)
	if err != nil {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}
	if username != "" && subtle.ConstantTimeCompare([]byte(username), []byte(physician.Username)) != 1 {
		http.Error(w, "physician session does not match requested user", http.StatusForbidden)
		return
	}

	prestacionID, err := a.loadAndesPrestacionIDByStudyUID(ctx, studyInstanceUID)
	if err != nil {
		a.log("error", "physician_andes_report_lookup_failed", map[string]any{
			"physician_id":       physician.ID,
			"study_instance_uid": studyInstanceUID,
			"error":              err.Error(),
		})
		http.Error(w, "failed to resolve andes report metadata", http.StatusInternalServerError)
		return
	}
	if prestacionID == "" {
		http.Error(w, "study has no associated andes prestacion", http.StatusNotFound)
		return
	}

	reportPDF, err := a.downloadAndesPrestacionReportPDF(ctx, prestacionID)
	if err != nil {
		a.log("error", "physician_andes_report_download_failed", map[string]any{
			"physician_id":        physician.ID,
			"study_instance_uid":  studyInstanceUID,
			"andes_prestacion_id": prestacionID,
			"error":               err.Error(),
		})
		http.Error(w, "failed to download andes report", http.StatusBadGateway)
		return
	}

	filename := "Informe-" + sanitizeFilename(studyInstanceUID) + ".pdf"
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(reportPDF)))
	if _, err := w.Write(reportPDF); err != nil {
		a.log("warn", "physician_andes_report_write_failed", map[string]any{
			"physician_id":        physician.ID,
			"study_instance_uid":  studyInstanceUID,
			"andes_prestacion_id": prestacionID,
			"error":               err.Error(),
		})
		return
	}

	a.log("info", "physician_andes_report_download_completed", map[string]any{
		"physician_id":        physician.ID,
		"study_instance_uid":  studyInstanceUID,
		"andes_prestacion_id": prestacionID,
		"size_bytes":          len(reportPDF),
	})
}

func (a *App) andesMetadataAvailableForSourceNode(nodeID string) bool {
	return a.sourceNodeUsesHIS(nodeID)
}

func (a *App) andesMetadataAvailableForPatientStudy(study PatientStudy) bool {
	return a.andesMetadataAvailableForSourceNode(study.SourceNodeID)
}

func (a *App) andesMetadataAvailableForPhysicianResult(result PhysicianResult) bool {
	sourceNodeID := a.resolveConfiguredNodeIDForStudy(result.SourceNodeID, result.Locations)
	return a.andesMetadataAvailableForSourceNode(sourceNodeID)
}

func buildPrestacionLookupSource(cfg ExternalConfig, logger *log.Logger) (PrestacionLookupSource, error) {
	if !cfg.HIS.PrestacionesEnrichmentEnabled {
		return &NoopPrestacionLookupSource{}, nil
	}

	mode := cfg.HIS.ResolvedPrestacionesProvider()

	composite := &CompositePrestacionLookupSource{mode: mode}

	mongoEligible := strings.EqualFold(strings.TrimSpace(cfg.HIS.Provider), "his_mongo_direct") &&
		(mode == HISPrestacionesProviderMongo || mode == HISPrestacionesProviderAuto)
	restEligible := mode == HISPrestacionesProviderREST || mode == HISPrestacionesProviderAuto

	if mongoEligible {
		mongoSource, err := connectMongoPrestacionLookupSource()
		if err != nil {
			if mode == HISPrestacionesProviderMongo {
				return nil, err
			}
			if logger != nil {
				logger.Printf("prestaciones lookup: mongo provider unavailable in %s mode: %v", mode, err)
			}
		} else if mongoSrc, ok := mongoSource.(*MongoPrestacionLookupSource); ok {
			composite.mongo = mongoSrc
		}
	}

	if restEligible {
		restSource, err := connectAndesRESTPrestacionLookupSource(cfg.HIS)
		if err != nil {
			if mode == HISPrestacionesProviderREST && composite.mongo == nil {
				return nil, err
			}
			if logger != nil {
				logger.Printf("prestaciones lookup: rest provider unavailable in %s mode: %v", mode, err)
			}
		} else {
			composite.rest = restSource
		}
	}

	if composite.rest == nil && composite.mongo == nil {
		return &NoopPrestacionLookupSource{}, nil
	}
	return composite, nil
}

func connectAndesRESTPrestacionLookupSource(cfg HISConfig) (*AndesRESTPrestacionLookupSource, error) {
	token := strings.TrimSpace(os.Getenv("HIS_TOKEN"))
	if token == "" {
		return nil, errors.New("missing HIS_TOKEN env var for andes rest prestaciones provider")
	}
	baseURL := strings.TrimSpace(os.Getenv("HIS_BASE_URL"))
	if baseURL == "" {
		baseURL = "https://app.andes.gob.ar/api"
	}
	timeout := cfg.ResolvedAndesRESTRequestTimeout()
	return &AndesRESTPrestacionLookupSource{
		httpClient: &http.Client{Timeout: timeout},
		baseURL:    baseURL,
		token:      token,
		timeout:    timeout,
	}, nil
}

func connectMongoPrestacionLookupSource() (PrestacionLookupSource, error) {
	mongoURI := strings.TrimSpace(os.Getenv("HIS_MONGO_URI"))
	mongoDatabase := strings.TrimSpace(os.Getenv("HIS_MONGO_DATABASE"))
	if mongoURI == "" || mongoDatabase == "" {
		return nil, errors.New("missing required mongo env vars for prestaciones lookup")
	}

	connectTimeout := durationFromEnv("HIS_MONGO_CONNECT_TIMEOUT_MS", 5000*time.Millisecond)
	queryTimeout := durationFromEnv("HIS_MONGO_QUERY_TIMEOUT_MS", 10000*time.Millisecond)
	batchSize := intFromEnv("HIS_MONGO_PRESTACIONES_BATCH_SIZE", 20)
	if batchSize <= 0 {
		batchSize = 20
	}

	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, fmt.Errorf("connect mongo prestaciones provider: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("ping mongo prestaciones provider: %w", err)
	}

	return &MongoPrestacionLookupSource{
		client:         client,
		collection:     client.Database(mongoDatabase).Collection("prestaciones"),
		connectTimeout: connectTimeout,
		queryTimeout:   queryTimeout,
		batchSize:      batchSize,
	}, nil
}

func uniqueConceptIDsFromTipoPrestaciones(values []PACSTipoPrestacionConfig) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		conceptID := strings.TrimSpace(value.ConceptID)
		if conceptID == "" {
			continue
		}
		if _, ok := seen[conceptID]; ok {
			continue
		}
		seen[conceptID] = struct{}{}
		out = append(out, conceptID)
	}
	return out
}

func (a *App) enrichPatientStudiesWithAndes(ctx context.Context, patientID string, studies []PatientStudy) error {
	if len(studies) == 0 {
		return nil
	}
	if err := a.applyPersistedQIDOCacheToPatientStudies(ctx, studies); err != nil {
		return err
	}

	prefixes := a.externalConfig.HIS.ResolvedAndesUIDPrefixes()
	missingStudyUIDs := make([]string, 0, len(studies))
	for _, study := range studies {
		if !a.andesMetadataAvailableForPatientStudy(study) {
			continue
		}
		if strings.TrimSpace(study.AndesPrestacionID) != "" || strings.TrimSpace(study.AndesPrestacion) != "" || strings.TrimSpace(study.AndesProfessional) != "" {
			continue
		}
		if !studyUIDLikelyAndesIssued(study.StudyInstanceUID, prefixes) {
			continue
		}
		missingStudyUIDs = append(missingStudyUIDs, study.StudyInstanceUID)
	}
	if len(missingStudyUIDs) == 0 {
		return nil
	}

	summaries, err := a.fetchPrestacionsForPatient(ctx, patientID, missingStudyUIDs)
	if err != nil {
		return err
	}
	for i := range studies {
		if summary, ok := summaries[studies[i].StudyInstanceUID]; ok {
			studies[i].AndesPrestacionID = summary.PrestacionID
			studies[i].AndesPrestacion = summary.PrestacionFSN
			studies[i].AndesProfessional = summary.Professional
		}
	}
	return nil
}

func (a *App) fetchPrestacionsForPatient(ctx context.Context, patientID string, candidateStudyUIDs []string) (map[string]AndesPrestacionSummary, error) {
	if len(candidateStudyUIDs) == 0 {
		return map[string]AndesPrestacionSummary{}, nil
	}
	mode := a.prestacionLookup.Mode()
	a.log("info", "andes_prestaciones_lookup_started", map[string]any{
		"patient_id":           patientID,
		"mode":                 mode,
		"candidate_study_uids": len(candidateStudyUIDs),
		"lookup_provider_name": a.prestacionLookup.ProviderName(),
	})
	switch mode {
	case HISPrestacionesProviderREST, HISPrestacionesProviderAuto:
		mongoID, err := a.loadPatientMongoObjectID(ctx, patientID)
		if err != nil {
			return nil, fmt.Errorf("load patient mongo id: %w", err)
		}
		if mongoID == "" {
			if mode == HISPrestacionesProviderAuto {
				return a.prestacionLookup.FindByStudyUIDs(ctx, candidateStudyUIDs)
			}
			a.log("warn", "andes_prestaciones_rest_skipped_no_mongo_id", map[string]any{
				"patient_id":           patientID,
				"candidate_study_uids": len(candidateStudyUIDs),
			})
			return map[string]AndesPrestacionSummary{}, nil
		}

		all, err := a.prestacionLookup.FindByPatientMongoID(ctx, mongoID, nil)
		if err != nil {
			a.log("error", "andes_prestaciones_rest_failed", map[string]any{
				"patient_id": patientID,
				"mongo_id":   mongoID,
				"error":      err.Error(),
			})
			if mode == HISPrestacionesProviderAuto {
				return a.prestacionLookup.FindByStudyUIDs(ctx, candidateStudyUIDs)
			}
			return nil, err
		}

		out := make(map[string]AndesPrestacionSummary, len(candidateStudyUIDs))
		for _, uid := range candidateStudyUIDs {
			if summary, ok := all[uid]; ok {
				out[uid] = summary
			}
		}
		if mode == HISPrestacionesProviderAuto {
			missing := make([]string, 0, len(candidateStudyUIDs))
			for _, uid := range candidateStudyUIDs {
				if _, ok := out[uid]; !ok {
					missing = append(missing, uid)
				}
			}
			if len(missing) > 0 {
				if mongoSummaries, mongoErr := a.prestacionLookup.FindByStudyUIDs(ctx, missing); mongoErr == nil {
					for k, v := range mongoSummaries {
						out[k] = v
					}
				} else {
					a.log("warn", "andes_prestaciones_mongo_fallback_failed", map[string]any{
						"patient_id": patientID,
						"error":      mongoErr.Error(),
					})
				}
			}
		}
		a.log("info", "andes_prestaciones_lookup_completed", map[string]any{
			"patient_id":           patientID,
			"mode":                 mode,
			"candidate_study_uids": len(candidateStudyUIDs),
			"resolved_study_uids":  len(out),
			"rest_docs_matched":    len(all),
		})
		return out, nil
	default:
		summaries, err := a.prestacionLookup.FindByStudyUIDs(ctx, candidateStudyUIDs)
		if err != nil {
			return nil, err
		}
		a.log("info", "andes_prestaciones_lookup_completed", map[string]any{
			"patient_id":           patientID,
			"mode":                 mode,
			"candidate_study_uids": len(candidateStudyUIDs),
			"resolved_study_uids":  len(summaries),
			"rest_docs_matched":    0,
		})
		return summaries, nil
	}
}

func (a *App) enrichPhysicianResultsWithAndes(ctx context.Context, results []PhysicianResult) error {
	if len(results) == 0 {
		return nil
	}
	if err := a.applyPersistedQIDOCacheToPhysicianResults(ctx, results); err != nil {
		return err
	}

	prefixes := a.externalConfig.HIS.ResolvedAndesUIDPrefixes()
	missingStudyUIDs := make([]string, 0, len(results))
	for i := range results {
		if strings.TrimSpace(results[i].AndesPrestacionID) != "" || strings.TrimSpace(results[i].AndesPrestacion) != "" || strings.TrimSpace(results[i].AndesProfessional) != "" {
			continue
		}
		if !a.andesMetadataAvailableForPhysicianResult(results[i]) {
			continue
		}
		if !studyUIDLikelyAndesIssued(results[i].StudyInstanceUID, prefixes) {
			continue
		}
		missingStudyUIDs = append(missingStudyUIDs, results[i].StudyInstanceUID)
	}

	if len(missingStudyUIDs) == 0 {
		return nil
	}

	summaries := make(map[string]AndesPrestacionSummary, len(missingStudyUIDs))
	mode := a.prestacionLookup.Mode()
	switch mode {
	case HISPrestacionesProviderREST, HISPrestacionesProviderAuto:
		studyUIDsByMongoID := make(map[string]map[string]struct{})
		conceptIDsByMongoID := make(map[string]map[string]struct{})
		mongoIDByDocument := make(map[string]string)
		nodeConceptIDs := make(map[string][]string)

		for i := range results {
			uid := strings.TrimSpace(results[i].StudyInstanceUID)
			if uid == "" {
				continue
			}
			if !studyUIDLikelyAndesIssued(uid, prefixes) {
				continue
			}
			if strings.TrimSpace(results[i].AndesPrestacionID) != "" || strings.TrimSpace(results[i].AndesPrestacion) != "" || strings.TrimSpace(results[i].AndesProfessional) != "" {
				continue
			}
			if !a.andesMetadataAvailableForPhysicianResult(results[i]) {
				continue
			}

			patientID := strings.TrimSpace(results[i].PatientID)
			if patientID == "" {
				continue
			}

			mongoID := normalizeMongoObjectIDCandidate(patientID)
			if mongoID == "" {
				documentNumber := normalizeDocumentNumberCandidate(patientID)
				if documentNumber == "" {
					continue
				}

				if cachedMongoID, ok := mongoIDByDocument[documentNumber]; ok {
					mongoID = cachedMongoID
				} else {
					cachedMongoID, err := a.loadCachedMongoObjectIDByDocument(ctx, documentNumber)
					if err != nil {
						a.log("warn", "physician_andes_document_cache_lookup_failed", map[string]any{
							"document_number": documentNumber,
							"error":           err.Error(),
						})
					}
					if cachedMongoID != "" {
						mongoID = cachedMongoID
					} else {
						identity, resolveErr := a.identitySource.ResolveByDocument(ctx, documentNumber)
						if resolveErr != nil {
							a.log("warn", "physician_andes_document_identity_lookup_failed", map[string]any{
								"document_number": documentNumber,
								"error":           resolveErr.Error(),
							})
						} else {
							mongoID = mongoObjectIDFromAlternateIdentifiers(identity.AlternateIDs)
						}
					}
					mongoIDByDocument[documentNumber] = mongoID
				}
			}

			if mongoID == "" {
				continue
			}

			if _, ok := studyUIDsByMongoID[mongoID]; !ok {
				studyUIDsByMongoID[mongoID] = make(map[string]struct{})
			}
			studyUIDsByMongoID[mongoID][uid] = struct{}{}

			sourceNodeID := strings.TrimSpace(results[i].SourceNodeID)
			if sourceNodeID == "" {
				sourceNodeID = a.resolveConfiguredNodeIDForStudy(results[i].SourceNodeID, results[i].Locations)
			}
			if sourceNodeID != "" {
				if _, ok := conceptIDsByMongoID[mongoID]; !ok {
					conceptIDsByMongoID[mongoID] = make(map[string]struct{})
				}
				conceptIDs, cached := nodeConceptIDs[sourceNodeID]
				if !cached {
					for _, node := range a.externalConfig.PACSNodes {
						if strings.EqualFold(strings.TrimSpace(node.ID), sourceNodeID) {
							conceptIDs = uniqueConceptIDsFromTipoPrestaciones(node.TipoPrestacion)
							break
						}
					}
					nodeConceptIDs[sourceNodeID] = conceptIDs
				}
				for _, conceptID := range conceptIDs {
					conceptIDsByMongoID[mongoID][conceptID] = struct{}{}
				}
			}
		}

		type restLookupResult struct {
			mongoID string
			matched map[string]AndesPrestacionSummary
			err     error
		}

		taskCount := len(studyUIDsByMongoID)
		attemptedCalls := 0
		successfulCalls := 0
		failedCalls := 0
		workerCount := a.andesEnrichWorkerConcurrency()
		if workerCount > taskCount {
			workerCount = taskCount
		}

		if taskCount > 0 && workerCount > 0 {
			mongoTasks := make(chan string, taskCount)
			resultsCh := make(chan restLookupResult, taskCount)

			for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
				go func() {
					for mongoID := range mongoTasks {
						conceptIDs := make([]string, 0, len(conceptIDsByMongoID[mongoID]))
						for conceptID := range conceptIDsByMongoID[mongoID] {
							conceptIDs = append(conceptIDs, conceptID)
						}
						sort.Strings(conceptIDs)
						all, err := a.prestacionLookup.FindByPatientMongoID(ctx, mongoID, conceptIDs)
						if err != nil {
							resultsCh <- restLookupResult{mongoID: mongoID, err: err}
							continue
						}
						matched := make(map[string]AndesPrestacionSummary)
						for uid := range studyUIDsByMongoID[mongoID] {
							if summary, ok := all[uid]; ok {
								matched[uid] = summary
							}
						}
						resultsCh <- restLookupResult{mongoID: mongoID, matched: matched}
					}
				}()
			}

			for mongoID := range studyUIDsByMongoID {
				mongoTasks <- mongoID
			}
			close(mongoTasks)

			for resultIndex := 0; resultIndex < taskCount; resultIndex++ {
				result := <-resultsCh
				attemptedCalls++
				if result.err != nil {
					failedCalls++
					a.log("warn", "physician_andes_rest_lookup_failed", map[string]any{
						"mongo_id": result.mongoID,
						"error":    result.err.Error(),
					})
					continue
				}
				successfulCalls++
				for uid, summary := range result.matched {
					summaries[uid] = summary
				}
			}
		}

		if mode == HISPrestacionesProviderAuto {
			fallbackUIDs := make([]string, 0, len(missingStudyUIDs))
			for _, uid := range missingStudyUIDs {
				if _, ok := summaries[uid]; ok {
					continue
				}
				fallbackUIDs = append(fallbackUIDs, uid)
			}
			if len(fallbackUIDs) > 0 {
				mongoSummaries, err := a.prestacionLookup.FindByStudyUIDs(ctx, fallbackUIDs)
				if err != nil {
					a.log("warn", "physician_andes_mongo_fallback_failed", map[string]any{
						"fallback_uid_count": len(fallbackUIDs),
						"error":              err.Error(),
					})
				} else {
					for uid, summary := range mongoSummaries {
						summaries[uid] = summary
					}
				}
			}
		}

		a.log("info", "physician_andes_lookup_completed", map[string]any{
			"mode":                 mode,
			"candidate_study_uids": len(missingStudyUIDs),
			"grouped_patients":     len(studyUIDsByMongoID),
			"resolved_study_uids":  len(summaries),
			"attempted_calls":      attemptedCalls,
			"successful_calls":     successfulCalls,
			"failed_calls":         failedCalls,
		})
	default:
		fetched, err := a.prestacionLookup.FindByStudyUIDs(ctx, missingStudyUIDs)
		if err != nil {
			return err
		}
		summaries = fetched
	}

	for i := range results {
		if summary, ok := summaries[results[i].StudyInstanceUID]; ok {
			results[i].AndesPrestacionID = summary.PrestacionID
			results[i].AndesPrestacion = summary.PrestacionFSN
			results[i].AndesProfessional = summary.Professional
		}
	}

	return nil
}

func (a *App) loadAndesPrestacionIDByStudyUID(ctx context.Context, studyInstanceUID string) (string, error) {
	studyInstanceUID = strings.TrimSpace(studyInstanceUID)
	if studyInstanceUID == "" {
		return "", nil
	}

	var prestacionID string
	err := a.db.QueryRowContext(ctx, `
		SELECT COALESCE(andes_prestacion_id, '')
		FROM qido_study_cache
		WHERE study_instance_uid = $1
		  AND NULLIF(andes_prestacion_id, '') IS NOT NULL
		ORDER BY last_andes_enriched_at DESC NULLS LAST, last_seen_at DESC NULLS LAST
		LIMIT 1
	`, studyInstanceUID).Scan(&prestacionID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(prestacionID), nil
}

func (a *App) downloadAndesPrestacionReportPDF(ctx context.Context, prestacionID string) ([]byte, error) {
	prestacionID = strings.TrimSpace(prestacionID)
	if prestacionID == "" {
		return nil, errors.New("andes report: empty prestacion id")
	}
	token := strings.TrimSpace(os.Getenv("HIS_TOKEN"))
	if token == "" {
		return nil, errors.New("andes report: missing HIS_TOKEN")
	}
	baseURL := strings.TrimSpace(os.Getenv("HIS_BASE_URL"))
	if baseURL == "" {
		baseURL = "https://app.andes.gob.ar/api"
	}

	endpoint := strings.TrimRight(baseURL, "/") + "/modules/descargas"
	payload, err := json.Marshal(map[string]string{
		"idPrestacion": prestacionID,
	})
	if err != nil {
		return nil, fmt.Errorf("andes report: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return nil, fmt.Errorf("andes report: build request: %w", err)
	}
	req.Header.Set("Authorization", "JWT "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, application/pdf")

	client := a.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("andes report: do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("andes report: read response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errors.New("andes report: unauthorized")
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("andes report: http %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	reportPDF, err := decodeAndesReportPayload(body)
	if err != nil {
		return nil, err
	}
	return reportPDF, nil
}

func decodeAndesReportPayload(body []byte) ([]byte, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return nil, errors.New("andes report: empty response")
	}

	// Some environments can return the PDF bytes directly.
	if strings.HasPrefix(trimmed, "%PDF-") {
		return []byte(trimmed), nil
	}

	candidates := make([]string, 0, 8)
	if decodedQuoted, err := strconv.Unquote(trimmed); err == nil {
		candidates = append(candidates, decodedQuoted)
	}
	candidates = append(candidates, trimmed)

	var decodedJSON any
	if err := json.Unmarshal([]byte(trimmed), &decodedJSON); err == nil {
		candidates = append(candidates, extractBase64Candidates(decodedJSON)...)
	}

	for _, candidate := range candidates {
		pdf, ok := decodeBase64PDF(candidate)
		if !ok {
			continue
		}
		return pdf, nil
	}

	return nil, errors.New("andes report: response did not contain a valid base64 PDF payload")
}
