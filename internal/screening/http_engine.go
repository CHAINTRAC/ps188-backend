package screening

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
)

// verifyPath is the endpoint exposed by passport-model/server.py
// (https://passport-model.onrender.com/docs).
const verifyPath = "/api/v1/verify"

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
}

// NewHTTPEngine builds the real screening client.
func NewHTTPEngine(baseURL, apiKey string, timeout time.Duration) Engine {
	return &httpEngine{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client:  &http.Client{Timeout: timeout},
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

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, apperr.ERRORS.ScreeningEngineUnavailable.Wrap(err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		detail := truncate(raw, 300)
		var er errorResponse
		if json.Unmarshal(raw, &er) == nil && er.Error.Code != "" {
			detail = er.Error.Code + ": " + er.Error.Message
		}
		return nil, apperr.ERRORS.ScreeningEngineUnavailable.Wrap(
			fmt.Errorf("engine status %d — %s", resp.StatusCode, detail))
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

	return &ScreenResult{
		Verdict:         verdict,
		DocType:         docTypeFromParam(pr.DocType),
		RiskScore:       pr.RiskScore,
		Reasons:         pr.Reasons,
		ExtractedFields: parseExtractedFields(pr.ExtractedFields),
		EvidenceItems:   evidence,
		RawEvidence:     pr.EvidenceTable,
	}, nil
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

// docTypeParam maps our doc types onto the values server.py accepts
// (ALLOWED_DOC_TYPES = {"auto", "passport", "aadhaar"}). Anything else is sent
// as "auto" and the pipeline routes it.
func docTypeParam(d model.DocType) string {
	switch d {
	case model.DocPassport:
		return "passport"
	case model.DocNationalID:
		return "aadhaar"
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
