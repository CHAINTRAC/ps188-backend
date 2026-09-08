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
)

type blacklistHandler struct {
	blacklist *service.BlacklistService
	maxUpload int64
}

// create is multipart, not JSON, since the photo is optional but the other
// fields still need the usual required/format checks — those now live in
// BlacklistService.Add.
func (h *blacklistHandler) create(c *gin.Context) {
	in := model.CreateBlacklistInput{
		Kind:        model.BlacklistKind(c.PostForm("kind")),
		DocNumber:   c.PostForm("doc_number"),
		DocType:     model.DocType(c.PostForm("doc_type")),
		Name:        c.PostForm("name"),
		DOB:         c.PostForm("dob"),
		Nationality: c.PostForm("nationality"),
		Reason:      c.PostForm("reason"),
		Source:      c.PostForm("source"),
	}

	if fh, err := c.FormFile("photo"); err == nil {
		if fh.Size > h.maxUpload {
			middleware.Fail(c, apperr.ERRORS.FileTooLarge)
			return
		}
		f, err := fh.Open()
		if err != nil {
			middleware.Fail(c, apperr.ERRORS.StorageFailed.Wrap(err))
			return
		}
		data, err := io.ReadAll(io.LimitReader(f, h.maxUpload+1))
		f.Close()
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
		in.Photo = data
		in.PhotoName = fh.Filename
	}

	actor, _ := middleware.Principal(c)
	view, err := h.blacklist.Add(c.Request.Context(), actor.UserID, c.ClientIP(), in)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusCreated, view, "Blacklist entry added")
}

func (h *blacklistHandler) list(c *gin.Context) {
	cursor, limit := pageParams(c)
	filter := model.BlacklistFilter{
		Kind:    model.BlacklistKind(c.Query("kind")),
		DocType: model.DocType(c.Query("doc_type")),
		Query:   c.Query("q"),
	}
	if v := c.Query("active"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			filter.Active = &b
		}
	}
	page, err := h.blacklist.List(c.Request.Context(), filter, cursor, limit)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Paginated(c, page, "Blacklist entries")
}

func (h *blacklistHandler) photo(c *gin.Context) {
	var buf bytes.Buffer
	name, err := h.blacklist.StreamPhoto(c.Request.Context(), c.Param("id"), &buf)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", "inline; filename=\""+name+"\"")
	c.Data(nethttp.StatusOK, nethttp.DetectContentType(buf.Bytes()), buf.Bytes())
}

func (h *blacklistHandler) get(c *gin.Context) {
	view, err := h.blacklist.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusOK, view, "Blacklist entry")
}

func (h *blacklistHandler) deactivate(c *gin.Context) {
	actor, _ := middleware.Principal(c)
	view, err := h.blacklist.Deactivate(c.Request.Context(), actor.UserID, c.ClientIP(), c.Param("id"))
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusOK, view, "Blacklist entry deactivated")
}

func (h *blacklistHandler) check(c *gin.Context) {
	probe := model.BlacklistProbe{
		DocNumber:   c.Query("doc_number"),
		Name:        c.Query("name"),
		DOB:         c.Query("dob"),
		Nationality: c.Query("nationality"),
	}
	res, err := h.blacklist.Check(c.Request.Context(), probe)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	response.Success(c, nethttp.StatusOK, res, "Blacklist check")
}
