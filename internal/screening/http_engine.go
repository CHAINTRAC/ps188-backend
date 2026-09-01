package screening

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
)

// httpEngine calls a hosted FastAPI wrapper around predict_pipeline.py:
//
//	POST {baseURL}/predict   (multipart/form-data)
//	  image, doc_type, doc_number, mrz_line1, mrz_line2
//	  header X-API-Key: {apiKey}
//	→ 200 {verdict, risk_score, reasons[], evidence_table{}, extracted_fields{}}
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
	Verdict         string            `json:"verdict"`
	RiskScore       float64           `json:"risk_score"`
	Reasons         []string          `json:"reasons"`
	ExtractedFields map[string]string `json:"extracted_fields"`
	EvidenceTable   map[string]any    `json:"evidence_table"`
}

func (e *httpEngine) Screen(ctx context.Context, req ScreenRequest) (*ScreenResult, error) {
	body, contentType, err := buildMultipart(req)
	if err != nil {
		return nil, apperr.ERRORS.ScreeningEngineUnavailable.Wrap(err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/predict", body)
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
		return nil, apperr.ERRORS.ScreeningEngineUnavailable.Wrap(
			fmt.Errorf("engine status %d: %s", resp.StatusCode, truncate(raw, 300)))
	}

	var pr predictResponse
	if err := json.Unmarshal(raw, &pr); err != nil {
		return nil, apperr.ERRORS.ScreeningEngineBadResponse.Wrap(err)
	}
	verdict := model.Verdict(strings.ToUpper(strings.TrimSpace(pr.Verdict)))
	if verdict == "" {
		return nil, apperr.ERRORS.ScreeningEngineBadResponse.Wrap(fmt.Errorf("empty verdict"))
	}
	return &ScreenResult{
		Verdict:         verdict,
		RiskScore:       pr.RiskScore,
		Reasons:         pr.Reasons,
		ExtractedFields: pr.ExtractedFields,
		Evidence:        pr.EvidenceTable,
	}, nil
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

// docTypeParam maps our doc types onto the pipeline's expected values. The
// pipeline currently branches on "passport" / "aadhar"; everything else it
// treats generically, so we pass "auto" and let it route.
func docTypeParam(d model.DocType) string {
	switch d {
	case model.DocPassport:
		return "passport"
	case model.DocNationalID:
		return "aadhar"
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
