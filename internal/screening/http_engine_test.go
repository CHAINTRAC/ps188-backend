package screening_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/screening"
)

// The external FastAPI model is the one boundary this project stubs — here via
// httptest. Everything else in a test runs for real.

func TestHTTPEngine_Screen_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Screen fans out to /extract-ocr in parallel for real field values —
		// give it a harmless response so it doesn't affect this verdict-focused test.
		if r.URL.Path == "/api/v1/extract-ocr" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"document_type":{"type":"passport"},"parsed_fields":{}}`))
			return
		}
		if r.URL.Path != "/api/v1/verify" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-API-Key") != "k3y" {
			t.Errorf("missing api key header")
		}
		_ = r.ParseMultipartForm(1 << 20)
		if r.FormValue("doc_type") != "passport" {
			t.Errorf("doc_type = %q", r.FormValue("doc_type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success":true,"filename":"p.jpg","doc_type":"passport",
			"verdict":"SUSPICIOUS","risk_score":0.42,
			"reasons":["MRZ checksum uncertain"],
			"evidence_table":{"cnn_score":0.6}
		}`))
	}))
	defer srv.Close()

	eng := screening.NewHTTPEngine(srv.URL, "k3y", 5*time.Second, nil)
	res, err := eng.Screen(context.Background(), screening.ScreenRequest{
		DocType: model.DocPassport, DocNumber: "Z1234567",
		Filename: "p.jpg", Image: []byte{0xff, 0xd8, 0xff},
	})
	if err != nil {
		t.Fatalf("screen: %v", err)
	}
	if res.Verdict != model.VerdictSuspicious || res.RiskScore != 0.42 {
		t.Fatalf("bad result: %+v", res)
	}
	if res.RawEvidence["cnn_score"] != 0.6 {
		t.Fatalf("raw evidence_table not passed through: %+v", res.RawEvidence)
	}
	// The model sent no toned evidence, so it is derived from reasons + risk.
	if len(res.EvidenceItems) != 1 || res.EvidenceItems[0].Text != "MRZ checksum uncertain" {
		t.Fatalf("derived evidence = %+v", res.EvidenceItems)
	}
	if res.EvidenceItems[0].Tone != model.EvidenceWarn { // risk 0.42 → warn band
		t.Fatalf("derived tone = %q, want warn", res.EvidenceItems[0].Tone)
	}
}

func TestHTTPEngine_Screen_StructuredFieldsAndTone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success":true,"verdict":"GENUINE","risk_score":0.05,
			"reasons":["all checks passed"],
			"extracted_fields":[
				{"label":"document_number","value":"Z1234567","confidence":0.97},
				{"label":"surname","value":"DOE","confidence":0.9}
			],
			"evidence":[{"tone":"good","text":"MRZ checksums valid"}],
			"evidence_table":{"cnn_score":0.1}
		}`))
	}))
	defer srv.Close()

	eng := screening.NewHTTPEngine(srv.URL, "", time.Second, nil)
	res, err := eng.Screen(context.Background(), screening.ScreenRequest{
		DocType: model.DocPassport, Filename: "p.jpg", Image: []byte{0xff, 0xd8, 0xff},
	})
	if err != nil {
		t.Fatalf("screen: %v", err)
	}
	if len(res.ExtractedFields) != 2 || res.ExtractedFields[0].Label != "document_number" ||
		res.ExtractedFields[0].Confidence != 0.97 {
		t.Fatalf("extracted_fields = %+v", res.ExtractedFields)
	}
	// The model supplied its own toned evidence — passed through, not derived.
	if len(res.EvidenceItems) != 1 || res.EvidenceItems[0].Tone != model.EvidenceGood ||
		res.EvidenceItems[0].Text != "MRZ checksums valid" {
		t.Fatalf("evidence not passed through: %+v", res.EvidenceItems)
	}
}

func TestHTTPEngine_Screen_FlatExtractedFieldsMap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success":true,"verdict":"GENUINE","risk_score":0.05,
			"extracted_fields":{"surname":"DOE","document_number":"Z1"}
		}`))
	}))
	defer srv.Close()

	eng := screening.NewHTTPEngine(srv.URL, "", time.Second, nil)
	res, err := eng.Screen(context.Background(), screening.ScreenRequest{
		DocType: model.DocPassport, Filename: "p.jpg", Image: []byte{0xff, 0xd8, 0xff},
	})
	if err != nil {
		t.Fatalf("screen: %v", err)
	}
	if len(res.ExtractedFields) != 2 || res.ExtractedFields[0].Label != "document_number" {
		t.Fatalf("flat map not normalised (sorted by label): %+v", res.ExtractedFields)
	}
}

func TestHTTPEngine_Screen_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":"MODEL_UNAVAILABLE","message":"pipeline not initialized"}}`))
	}))
	defer srv.Close()

	eng := screening.NewHTTPEngine(srv.URL, "", time.Second, nil)
	_, err := eng.Screen(context.Background(), screening.ScreenRequest{
		DocType: model.DocPassport, Filename: "p.jpg", Image: []byte{1},
	})
	if ae := apperr.From(err); ae == nil || ae.Code != apperr.ERRORS.ScreeningEngineUnavailable.Code {
		t.Fatalf("want ScreeningEngineUnavailable, got %v", err)
	}
	if !strings.Contains(err.Error(), "MODEL_UNAVAILABLE") {
		t.Fatalf("want the model error code surfaced, got %v", err)
	}
}

// The model only accepts doc_type ∈ {auto, passport, aadhaar}; our national_id
// must map to "aadhaar" (not the pipeline-internal "aadhar").
func TestHTTPEngine_Screen_NationalIDDocType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		if got := r.FormValue("doc_type"); got != "aadhaar" {
			t.Errorf("doc_type = %q, want aadhaar", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"filename":"a.jpg","doc_type":"aadhaar","verdict":"GENUINE","risk_score":0.1}`))
	}))
	defer srv.Close()

	eng := screening.NewHTTPEngine(srv.URL, "", time.Second, nil)
	if _, err := eng.Screen(context.Background(), screening.ScreenRequest{
		DocType: model.DocNationalID, Filename: "a.jpg", Image: []byte{0xff, 0xd8, 0xff},
	}); err != nil {
		t.Fatalf("screen: %v", err)
	}
}

func TestHTTPEngine_Screen_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	eng := screening.NewHTTPEngine(srv.URL, "", time.Second, nil)
	_, err := eng.Screen(context.Background(), screening.ScreenRequest{
		DocType: model.DocPassport, Filename: "p.jpg", Image: []byte{1},
	})
	if ae := apperr.From(err); ae == nil || ae.Code != apperr.ERRORS.ScreeningEngineBadResponse.Code {
		t.Fatalf("want ScreeningEngineBadResponse, got %v", err)
	}
}
