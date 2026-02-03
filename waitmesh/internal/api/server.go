package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/waitmesh/waitmesh/internal/config"
	"github.com/waitmesh/waitmesh/internal/domain"
	"github.com/waitmesh/waitmesh/internal/engine"
	"github.com/waitmesh/waitmesh/internal/metrics"
	"github.com/waitmesh/waitmesh/internal/storage/sqlite"
)

type Server struct {
	eng    *engine.Engine
	cfg    config.Config
	log    *slog.Logger
	mux    *http.ServeMux
	started time.Time
}

func New(eng *engine.Engine, cfg config.Config, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{eng: eng, cfg: cfg, log: log, mux: http.NewServeMux(), started: time.Now().UTC()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.withMiddleware(s.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("POST /v1/events", s.postEvent)
	s.mux.HandleFunc("GET /v1/events", s.listEvents)
	s.mux.HandleFunc("POST /v1/rules", s.postRule)
	s.mux.HandleFunc("GET /v1/rules", s.listRules)
	s.mux.HandleFunc("GET /v1/rules/{id}", s.getRule)
	s.mux.HandleFunc("PATCH /v1/rules/{id}", s.patchRule)
	s.mux.HandleFunc("DELETE /v1/rules/{id}", s.deleteRule)
	s.mux.HandleFunc("GET /v1/pending", s.listPending)
	s.mux.HandleFunc("GET /v1/firings", s.listFirings)
	s.mux.HandleFunc("GET /v1/outputs", s.listOutputs)
	s.mux.HandleFunc("POST /v1/consumers", s.registerConsumer)
	s.mux.HandleFunc("POST /v1/outputs/poll", s.pollOutputs)
	s.mux.HandleFunc("POST /v1/outputs/ack", s.ackOutput)
	s.mux.HandleFunc("POST /v1/outputs/nack", s.nackOutput)
	s.mux.HandleFunc("POST /v1/outputs/purge", s.purgeOutputs)
	s.mux.HandleFunc("GET /v1/health/live", s.live)
	s.mux.HandleFunc("GET /v1/health/ready", s.ready)
	s.mux.Handle("GET /v1/metrics", metrics.Handler())
	s.mux.HandleFunc("GET /openapi.yaml", s.openapi)
}

func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
		// Liveness/readiness stay unauthenticated so orchestrators can probe safely.
		if s.cfg.AuthToken != "" && !isPublicHealthPath(r.URL.Path) {
			if !bearerAuthorized(r.Header.Get("Authorization"), s.cfg.AuthToken) {
				writeErr(w, http.StatusUnauthorized, domain.ErrUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isPublicHealthPath(path string) bool {
	return path == "/v1/health/live" || path == "/v1/health/ready"
}

func bearerAuthorized(header, token string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got := header[len(prefix):]
	if len(got) != len(token) {
		// Still compare to reduce trivial timing oracles on length alone.
		subtle.ConstantTimeCompare([]byte(got), []byte(token))
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

func (s *Server) postEvent(w http.ResponseWriter, r *http.Request) {
	var in domain.EventInput
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	start := time.Now()
	res, err := s.eng.Ingest(r.Context(), in)
	metrics.IngestLatency.Observe(time.Since(start).Seconds())
	if err != nil {
		metrics.EventsIngested.WithLabelValues("error").Inc()
		mapErr(w, err)
		return
	}
	if res.Idempotent {
		metrics.EventsIngested.WithLabelValues("idempotent").Inc()
		writeJSON(w, http.StatusOK, res)
		return
	}
	metrics.EventsIngested.WithLabelValues("created").Inc()
	writeJSON(w, http.StatusCreated, res)
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	evs, err := sqlite.ListEvents(r.Context(), s.eng.DB(), limit)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": evs})
}

func (s *Server) postRule(w http.ResponseWriter, r *http.Request) {
	var in domain.RuleInput
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rule, err := s.eng.CreateRule(r.Context(), in)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

func (s *Server) listRules(w http.ResponseWriter, r *http.Request) {
	rules, err := sqlite.ListRules(r.Context(), s.eng.DB())
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

func (s *Server) getRule(w http.ResponseWriter, r *http.Request) {
	rule, err := sqlite.GetRule(r.Context(), s.eng.DB().SQL, r.PathValue("id"))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (s *Server) patchRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body map[string]any
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if en, ok := body["enabled"].(bool); ok && len(body) == 1 {
		if err := s.eng.SetRuleEnabled(r.Context(), id, en); err != nil {
			mapErr(w, err)
			return
		}
		rule, err := sqlite.GetRule(r.Context(), s.eng.DB().SQL, id)
		if err != nil {
			mapErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, rule)
		return
	}
	b, _ := json.Marshal(body)
	var in domain.RuleInput
	if err := json.Unmarshal(b, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rule, err := s.eng.UpdateRule(r.Context(), id, in)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	if err := s.eng.DeleteRule(r.Context(), r.PathValue("id")); err != nil {
		mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listPending(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := sqlite.ListPending(r.Context(), s.eng.DB(), limit)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pending": items})
}

func (s *Server) listFirings(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := sqlite.ListFirings(r.Context(), s.eng.DB(), limit)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"firings": items})
}

func (s *Server) listOutputs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := sqlite.ListOutputs(r.Context(), s.eng.DB(), limit)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"outputs": items})
}

type pollReq struct {
	ConsumerID string `json:"consumer_id"`
	Limit      int    `json:"limit"`
	LeaseSecs  int    `json:"lease_seconds"`
}

func (s *Server) pollOutputs(w http.ResponseWriter, r *http.Request) {
	var req pollReq
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	lease := time.Duration(req.LeaseSecs) * time.Second
	dels, err := s.eng.PollOutputs(r.Context(), req.ConsumerID, req.Limit, lease)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deliveries": dels})
}

type ackReq struct {
	ConsumerID string `json:"consumer_id"`
	OutputID   string `json:"output_id"`
}

func (s *Server) ackOutput(w http.ResponseWriter, r *http.Request) {
	var req ackReq
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.eng.AckOutput(r.Context(), req.ConsumerID, req.OutputID); err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "acked"})
}

type consumerReq struct {
	ConsumerID string `json:"consumer_id"`
}

func (s *Server) registerConsumer(w http.ResponseWriter, r *http.Request) {
	var req consumerReq
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.eng.RegisterConsumer(r.Context(), req.ConsumerID); err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"consumer_id": req.ConsumerID, "status": "registered"})
}

func (s *Server) nackOutput(w http.ResponseWriter, r *http.Request) {
	var req ackReq
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.eng.NackOutput(r.Context(), req.ConsumerID, req.OutputID); err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "nacked"})
}

type purgeReq struct {
	RetainSeconds int `json:"retain_seconds"`
}

func (s *Server) purgeOutputs(w http.ResponseWriter, r *http.Request) {
	var req purgeReq
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	n, err := s.eng.PurgeRetainedOutbox(r.Context(), time.Duration(req.RetainSeconds)*time.Second)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"purged": n})
}

func (s *Server) live(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "uptime_sec": time.Since(s.started).Seconds()})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if !s.eng.Ready() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
		return
	}
	if err := s.eng.DB().SQL.PingContext(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db_unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func mapErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidEvent), errors.Is(err, domain.ErrInvalidRule):
		writeErr(w, http.StatusBadRequest, err)
	case errors.Is(err, domain.ErrEventConflict):
		writeErr(w, http.StatusConflict, err)
	case errors.Is(err, domain.ErrEventNotFound), errors.Is(err, domain.ErrRuleNotFound), errors.Is(err, domain.ErrLeaseNotFound):
		writeErr(w, http.StatusNotFound, err)
	case errors.Is(err, domain.ErrQueueFull):
		writeErr(w, http.StatusServiceUnavailable, err)
	case errors.Is(err, domain.ErrUnauthorized):
		writeErr(w, http.StatusUnauthorized, err)
	case errors.Is(err, domain.ErrConsumerRequired):
		writeErr(w, http.StatusBadRequest, err)
	default:
		if strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
	}
}

func (s *Server) openapi(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = io.WriteString(w, openAPIDoc)
}

const openAPIDoc = `openapi: 3.0.3
info:
  title: Waitmesh API
  version: 0.1.0
  description: Event condition engine — know when something didn't happen.
servers:
  - url: http://127.0.0.1:7777
paths:
  /v1/events:
    post:
      summary: Ingest event
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [id, type, key]
              properties:
                id: {type: string}
                type: {type: string}
                key: {type: string}
                data: {}
                producer_timestamp: {type: string, format: date-time}
      responses:
        '201': {description: Created}
        '200': {description: Idempotent}
        '409': {description: Conflict}
    get:
      summary: List recent events
      responses:
        '200': {description: OK}
  /v1/rules:
    post:
      summary: Create rule
      responses:
        '201': {description: Created}
    get:
      summary: List rules
      responses:
        '200': {description: OK}
  /v1/rules/{id}:
    get:
      summary: Get rule
      parameters:
        - in: path
          name: id
          required: true
          schema: {type: string}
      responses:
        '200': {description: OK}
    patch:
      summary: Update or enable/disable rule
      responses:
        '200': {description: OK}
    delete:
      summary: Delete rule
      responses:
        '204': {description: Deleted}
  /v1/pending:
    get:
      summary: List pending matches
      responses:
        '200': {description: OK}
  /v1/firings:
    get:
      summary: List firings
      responses:
        '200': {description: OK}
  /v1/outputs:
    get:
      summary: List output events
      responses:
        '200': {description: OK}
  /v1/consumers:
    post:
      summary: Register durable consumer
      responses:
        '201': {description: Registered}
  /v1/outputs/poll:
    post:
      summary: Poll outbox for a consumer (lease)
      responses:
        '200': {description: OK}
  /v1/outputs/ack:
    post:
      summary: Acknowledge delivery
      responses:
        '200': {description: OK}
  /v1/outputs/nack:
    post:
      summary: Release lease for retry
      responses:
        '200': {description: OK}
  /v1/outputs/purge:
    post:
      summary: Purge outbox rows acked by all registered consumers
      responses:
        '200': {description: OK}
  /v1/health/live:
    get:
      summary: Liveness (no auth)
      responses:
        '200': {description: OK}
  /v1/health/ready:
    get:
      summary: Readiness (no auth)
      responses:
        '200': {description: OK}
  /v1/metrics:
    get:
      summary: Prometheus metrics
      responses:
        '200': {description: OK}
`
