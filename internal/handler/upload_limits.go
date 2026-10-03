package handler

import (
	"errors"
	"mime"
	"net/http"

	"github.com/gin-gonic/gin"
)

const uploadFormOverhead = 1 << 20

// Bound the entire request before PostForm/FormFile can spill it to disk.
// Individual file checks remain necessary: the allowance is for form metadata,
// not an increase to the supported file size. URL-encoded certificate submissions
// are supported for the direct-upload/object-key flow.
func parseLimitedUploadForm(c *gin.Context, maxFileBytes int64) bool {
	maxRequestBytes := maxFileBytes + uploadFormOverhead
	if c.Request.ContentLength > maxRequestBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": 413, "message": "上传请求过大"})
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBytes)
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err == nil {
		switch mediaType {
		case "multipart/form-data":
			err = c.Request.ParseMultipartForm(8 << 20)
		case "application/x-www-form-urlencoded":
			err = c.Request.ParseForm()
		default:
			err = errors.New("unsupported upload content type")
		}
	}
	if err == nil {
		return true
	}
	var sizeErr *http.MaxBytesError
	if errors.As(err, &sizeErr) {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": 413, "message": "上传请求过大"})
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "上传表单格式错误"})
	}
	return false
}
