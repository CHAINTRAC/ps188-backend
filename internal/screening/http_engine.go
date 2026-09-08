package screening

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
)

// Endpoints exposed by passport-model/server.py (see /docs on the deployed
// instance for the live OpenAPI schema).
const (
	verifyPath     = "/api/v1/verify"
	extractOCRPath = "/api/v1/extract-ocr"
	matchFacePath  = "/api/v1/match-face"
)

// httpEngine calls a hosted FastAPI wrapper around predict_pipeline.py:
//
//	POST {baseURL}/api/v1/verify   (multipart/form-data)
//	  image, doc_type (auto|passport|aadhaar), doc_number, mrz_line1, mrz_line2
//	  header X-API-Key: {apiKey}
//	→ 200 {success, filename, doc_type, verdict, risk_score, reasons[], evidence_table{}}
//	→ 4xx/5xx {success:false, error:{code, message}}
type httpEngine struct {
	baseURL string
	apiKey  string
	client  *http.Client
	log     *slog.Logger
}

// NewHTTPEngine builds the real screening client.
func NewHTTPEngine(baseURL, apiKey string, timeout time.Duration, log *slog.Logger) Engine {
	if log == nil {
		log = slog.Default()
	}
	return &httpEngine{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client:  &http.Client{Timeout: timeout},
		log:     log,
	}
}

type predictResponse struct {
	Success   bool     `json:"success"`
	Filename  string   `json:"filename"`
	DocType   string   `json:"doc_type"`
	Verdict   string   `json:"verdict"`
	RiskScore float64  `json:"risk_score"`
	Reasons   []string `json:"reasons"`
	// ExtractedFields accepts either the structured form
	// [{label,value,confidence}, …] or a plain {label: value} map — see
	// parseExtractedFields. Absent today; real OCR fields arrive with Phase D.
	ExtractedFields json.RawMessage `json:"extracted_fields"`
	// Evidence, when the model supplies it, is the toned list [{tone,text}, …].
	Evidence      []model.EvidenceItem `json:"evidence"`
	EvidenceTable map[string]any       `json:"evidence_table"`
}

// errorResponse is the model's failure envelope: {"success":false,"error":{"code","message"}}.
type errorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (e *httpEngine) Screen(ctx context.Context, req ScreenRequest) (*ScreenResult, error) {
	body, contentType, err := buildMultipart(req)
	if err != nil {
		return nil, apperr.ERRORS.ScreeningEngineUnavailable.Wrap(err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+verifyPath, body)
	if err != nil {
		return nil, apperr.ERRORS.ScreeningEngineUnavailable.Wrap(err)
	}
	httpReq.Header.Set("Content-Type", contentType)
	if e.apiKey != "" {
		httpReq.Header.Set("X-API-Key", e.apiKey)
	}

	// /verify never carries raw field values (doc_number, name, dates) — those
	// only come from /extract-ocr, fetched here in parallel and merged in below.
	var (
		wg         sync.WaitGroup
		ocrFields  []model.ExtractedField
		verifyResp *http.Response
		verifyErr  error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		ocrFields = e.extractOCR(ctx, req)
	}()

	verifyResp, verifyErr = e.client.Do(httpReq)
	wg.Wait()

	if verifyErr != nil {
		return nil, apperr.ERRORS.ScreeningEngineUnavailable.Wrap(verifyErr)
	}
	defer verifyResp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(verifyResp.Body, 4<<20))
	if verifyResp.StatusCode != http.StatusOK {
		detail := truncate(raw, 300)
		var er errorResponse
		if json.Unmarshal(raw, &er) == nil && er.Error.Code != "" {
			detail = er.Error.Code + ": " + er.Error.Message
		}
		return nil, apperr.ERRORS.ScreeningEngineUnavailable.Wrap(
			fmt.Errorf("engine status %d — %s", verifyResp.StatusCode, detail))
	}

	var pr predictResponse
	if err := json.Unmarshal(raw, &pr); err != nil {
		return nil, apperr.ERRORS.ScreeningEngineBadResponse.Wrap(err)
	}
	verdict := model.Verdict(strings.ToUpper(strings.TrimSpace(pr.Verdict)))
	if verdict == "" {
		return nil, apperr.ERRORS.ScreeningEngineBadResponse.Wrap(fmt.Errorf("empty verdict"))
	}

	// Prefer the model's own toned evidence; derive it from reasons + risk when
	// the model does not supply one.
	evidence := pr.Evidence
	if len(evidence) == 0 {
		evidence = DeriveEvidence(pr.Reasons, pr.RiskScore)
	}

	fields := parseExtractedFields(pr.ExtractedFields)
	if len(fields) == 0 {
		fields = ocrFields
	}

	return &ScreenResult{
		Verdict:         verdict,
		DocType:         docTypeFromParam(pr.DocType),
		RiskScore:       pr.RiskScore,
		Reasons:         pr.Reasons,
		ExtractedFields: fields,
		EvidenceItems:   evidence,
		RawEvidence:     pr.EvidenceTable,
	}, nil
}

type ocrExtractResponse struct {
	Success      bool           `json:"success"`
	ParsedFields map[string]any `json:"parsed_fields"`
}

// extractOCR failures are logged and swallowed — the fields are an
// enhancement, not a requirement for the verdict.
func (e *httpEngine) extractOCR(ctx context.Context, req ScreenRequest) []model.ExtractedField {
	body, contentType, err := buildOCRMultipart(req)
	if err != nil {
		e.log.WarnContext(ctx, "extract-ocr: building request failed", slog.String("error", err.Error()))
		return nil
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+extractOCRPath, body)
	if err != nil {
		e.log.WarnContext(ctx, "extract-ocr: building request failed", slog.String("error", err.Error()))
		return nil
	}
	httpReq.Header.Set("Content-Type", contentType)
	if e.apiKey != "" {
		httpReq.Header.Set("X-API-Key", e.apiKey)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		e.log.WarnContext(ctx, "extract-ocr: request failed", slog.String("error", err.Error()))
		return nil
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode != http.StatusOK {
		e.log.WarnContext(ctx, "extract-ocr: non-200 response", slog.Int("status", resp.StatusCode))
		return nil
	}

	var or ocrExtractResponse
	if err := json.Unmarshal(raw, &or); err != nil {
		e.log.WarnContext(ctx, "extract-ocr: bad JSON", slog.String("error", err.Error()))
		return nil
	}

	fields := make([]model.ExtractedField, 0, len(or.ParsedFields))
	for label, v := range or.ParsedFields {
		if v == nil {
			continue
		}
		var value string
		switch t := v.(type) {
		case string:
			value = t
		case bool, float64:
			value = fmt.Sprintf("%v", t)
		default:
			continue // skip nested objects (e.g. mrz raw blob) — not a flat field
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		fields = append(fields, model.ExtractedField{Label: label, Value: value})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Label < fields[j].Label })
	return fields
}

func (e *httpEngine) MatchFace(ctx context.Context, req FaceMatchRequest) (*FaceMatchResult, error) {
	body, contentType, err := buildFaceMatchMultipart(req)
	if err != nil {
		return nil, apperr.ERRORS.ScreeningEngineUnavailable.Wrap(err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+matchFacePath, body)
	if err != nil {
		return nil, apperr.ERRORS.ScreeningEngineUnavailable.Wrap(err)
	}
	httpReq.Header.Set("Content-Type", contentType)
	if e.apiKey != "" {
		httpReq.Header.Set("X-API-Key", e.apiKey)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, apperr.ERRORS.ScreeningEngineUnavailable.Wrap(err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		detail := truncate(raw, 300)
		var er errorResponse
		if json.Unmarshal(raw, &er) == nil && er.Error.Code != "" {
			detail = er.Error.Code + ": " + er.Error.Message
		}
		return nil, apperr.ERRORS.ScreeningEngineUnavailable.Wrap(
			fmt.Errorf("face-match status %d — %s", resp.StatusCode, detail))
	}

	var fr faceMatchResponse
	if err := json.Unmarshal(raw, &fr); err != nil {
		return nil, apperr.ERRORS.ScreeningEngineBadResponse.Wrap(err)
	}

	return &FaceMatchResult{
		IsMatch:         fr.IsMatch,
		SimilarityScore: fr.SimilarityScore,
		Threshold:       fr.Threshold,
		Message:         fr.Message,
	}, nil
}

type faceMatchResponse struct {
	Success         bool    `json:"success"`
	IsMatch         bool    `json:"is_match"`
	SimilarityScore float64 `json:"similarity_score"`
	Threshold       float64 `json:"threshold"`
	Message         string  `json:"message"`
}

// docTypeFromParam is the inverse of docTypeParam — it maps the model's
// classification back onto our DocType. Unknown / "auto" / empty yields "".
func docTypeFromParam(s string) model.DocType {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "passport":
		return model.DocPassport
	case "aadhaar", "aadhar", "national_id":
		return model.DocNationalID
	case "visa":
		return model.DocVisa
	case "driving_license", "driving-licence", "dl":
		return model.DocDrivingLicense
	case "permit":
		return model.DocPermit
	default:
		return ""
	}
}

// parseExtractedFields accepts either [{label,value,confidence}, …] or a plain
// {label: value} object and normalises both to []model.ExtractedField. An
// unrecognised or empty payload yields nil — the caller falls back to the raw
// evidence_table.
func parseExtractedFields(raw json.RawMessage) []model.ExtractedField {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var structured []model.ExtractedField
	if err := json.Unmarshal(raw, &structured); err == nil && len(structured) > 0 {
		out := structured[:0]
		for _, f := range structured {
			if strings.TrimSpace(f.Label) == "" {
				continue
			}
			out = append(out, f)
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	var flat map[string]string
	if err := json.Unmarshal(raw, &flat); err == nil && len(flat) > 0 {
		out := make([]model.ExtractedField, 0, len(flat))
		for k, v := range flat {
			out = append(out, model.ExtractedField{Label: k, Value: v})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
		return out
	}
	return nil
}

func buildMultipart(req ScreenRequest) (io.Reader, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	fw, err := w.CreateFormFile("image", req.Filename)
	if err != nil {
		return nil, "", err
	}
	if _, err := fw.Write(req.Image); err != nil {
		return nil, "", err
	}

	fields := map[string]string{
		"doc_type":   docTypeParam(req.DocType),
		"doc_number": req.DocNumber,
		"mrz_line1":  req.MRZLine1,
		"mrz_line2":  req.MRZLine2,
	}
	for k, v := range fields {
		if v == "" {
			continue
		}
		if err := w.WriteField(k, v); err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return &buf, w.FormDataContentType(), nil
}

// buildOCRMultipart mirrors buildMultipart for /extract-ocr, which only takes
// the image and doc_type (no doc_number/MRZ hints).
func buildOCRMultipart(req ScreenRequest) (io.Reader, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	fw, err := w.CreateFormFile("image", req.Filename)
	if err != nil {
		return nil, "", err
	}
	if _, err := fw.Write(req.Image); err != nil {
		return nil, "", err
	}
	if dt := docTypeParam(req.DocType); dt != "" {
		if err := w.WriteField("doc_type", dt); err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return &buf, w.FormDataContentType(), nil
}

// buildFaceMatchMultipart builds the /match-face request: selfie + document
// image, both required by the model's schema.
func buildFaceMatchMultipart(req FaceMatchRequest) (io.Reader, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	sf, err := w.CreateFormFile("selfie", req.SelfieFilename)
	if err != nil {
		return nil, "", err
	}
	if _, err := sf.Write(req.Selfie); err != nil {
		return nil, "", err
	}

	df, err := w.CreateFormFile("passport_image", req.DocFilename)
	if err != nil {
		return nil, "", err
	}
	if _, err := df.Write(req.DocImage); err != nil {
		return nil, "", err
	}

	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return &buf, w.FormDataContentType(), nil
}

// docTypeParam maps our doc types onto the values server.py accepts
// (ALLOWED_DOC_TYPES = {"auto", "passport", "aadhaar", "dl"}). Anything else is
// sent as "auto" and the pipeline routes it.
func docTypeParam(d model.DocType) string {
	switch d {
	case model.DocPassport:
		return "passport"
	case model.DocNationalID:
		return "aadhaar"
	case model.DocDrivingLicense:
		return "dl"
	default:
		return "auto"
	}
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
