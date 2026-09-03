package http

import (
	"bytes"
	"io"
	nethttp "net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/middleware"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/response"
	"github.com/sih26/ps188-backend/internal/service"
	"github.com/sih26/ps188-backend/internal/validate"
)

type screeningHandler struct {
	screenings *service.ScreeningService
	maxUpload  int64
}

// submit handles the multipart document upload and runs the screening.
func (h *screeningHandler) submit(c *gin.Context) {
	fh, err := c.FormFile("document")
	if err != nil {
		middleware.Fail(c, apperr.ERRORS.FileRequired)
		return
	}
	if fh.Size > h.maxUpload {
		middleware.Fail(c, apperr.ERRORS.FileTooLarge)
		return
	}

	f, err := fh.Open()
	if err != nil {
		middleware.Fail(c, apperr.ERRORS.StorageFailed.Wrap(err))
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, h.maxUpload+1))
	if err != nil {
		middleware.Fail(c, apperr.ERRORS.StorageFailed.Wrap(err))
		return
	}
	if int64(len(data)) > h.maxUpload {
		middleware.Fail(c, apperr.ERRORS.FileTooLarge)
		return
	}
	if ct := nethttp.DetectContentType(data); ct != "image/jpeg" && ct != "image/png" {
		middleware.Fail(c, apperr.ERRORS.InvalidFileType)
		return
	}

	actor, _ := middleware.Principal(c)
	// The verifier's checkpoint and region come from the token (stamped at
	// account creation), not the request — the officer works one checkpoint.
	checkpointID := actor.CheckpointID
	if checkpointID == "" {
		checkpointID = c.PostForm("checkpoint_id")
	}
	view, err := h.screenings.Submit(c.Request.Context(), service.SubmitInput{
		OfficerID:    actor.UserID,
		IP:           c.ClientIP(),
		CheckpointID: checkpointID,
		Region:       actor.Region,
		DocType:      model.DocType(c.PostForm("doc_type")),
		DocNumber:    c.PostForm("doc_number"),
		MRZLine1:     c.PostForm("mrz_line1"),
		MRZLine2:     c.PostForm("mrz_line2"),
		HolderName:   c.PostForm("holder_name"),
		DOB:          c.PostForm("dob"),
		Nationality:  c.PostForm("nationality"),
		ExpiryDate:   c.PostForm("expiry_date"),
		ImageName:    fh.Filename,
		Image:        data,
	})
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusCreated, view, "Screening submitted")
}

func (h *screeningHandler) list(c *gin.Context) {
	cursor, limit := pageParams(c)
	filter := model.ScreeningFilter{
		Verdict:       model.Verdict(c.Query("verdict")),
		DocType:       model.DocType(c.Query("doc_type")),
		Status:        model.ScreeningStatus(c.Query("status")),
		CheckpointID:  c.Query("checkpoint_id"),
		DecisionValue: model.Decision(c.Query("decision")),
	}
	if v := c.Query("decided"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			filter.Decided = &b
		}
	}
	// Region/officer scoping is derived from the principal, not the client:
	// verifier → own history, admin → own region, super admin → unscoped.
	middleware.ScopeToActor(c, &filter)

	page, err := h.screenings.List(c.Request.Context(), filter, cursor, limit)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Paginated(c, page, "Screenings")
}

func (h *screeningHandler) get(c *gin.Context) {
	view, err := h.screenings.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusOK, view, "Screening")
}

func (h *screeningHandler) image(c *gin.Context) {
	// Buffer the blob so a mid-stream storage error still produces a clean JSON
	// error envelope instead of a half-written 200 body.
	var buf bytes.Buffer
	name, err := h.screenings.StreamImage(c.Request.Context(), c.Param("id"), &buf)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", "inline; filename=\""+name+"\"")
	c.Data(nethttp.StatusOK, nethttp.DetectContentType(buf.Bytes()), buf.Bytes())
}

type decisionBody struct {
	Decision string `json:"decision" binding:"required"`
	Reason   string `json:"reason" binding:"required,min=1,max=1000"`
}

func (h *screeningHandler) decide(c *gin.Context) {
	var body decisionBody
	if err := validate.BindJSON(c, &body); err != nil {
		middleware.Fail(c, err)
		return
	}
	actor, _ := middleware.Principal(c)
	view, err := h.screenings.Decide(c.Request.Context(), c.Param("id"),
		actor.UserID, c.ClientIP(), model.Decision(body.Decision), body.Reason)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusOK, view, "Decision recorded")
}
