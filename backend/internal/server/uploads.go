package server

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Rogue324/AntigravityOnFnOs/backend/internal/session"
)

const maxUploadSize = 20 << 20 // 20 MB per file

var safeSegmentRe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitizeSegment(s string) string {
	s = strings.TrimSpace(filepath.Base(s))
	s = safeSegmentRe.ReplaceAllString(s, "_")
	s = strings.Trim(s, "._")
	if s == "" {
		return "file"
	}
	if len(s) > 80 {
		ext := filepath.Ext(s)
		s = s[:80-len(ext)] + ext
	}
	return s
}

func (s *Server) uploadsBaseDir() string {
	return filepath.Join(s.cfgStore.Paths().DataDir, "uploads")
}

func (s *Server) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize+1024*1024)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeError(w, http.StatusBadRequest, "文件过大或上传格式错误（单文件上限 20MB）")
		return
	}

	sessionID := sanitizeSegment(r.FormValue("session_id"))
	if sessionID == "" || sessionID == "file" {
		sessionID = "shared"
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "缺少上传文件")
		return
	}
	defer file.Close()

	// Peek first 512 bytes for MIME detection
	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	head = head[:n]
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(head)
	}
	if extMime := mime.TypeByExtension(strings.ToLower(filepath.Ext(header.Filename))); extMime != "" && strings.HasPrefix(mimeType, "application/octet-stream") {
		mimeType = extMime
	}

	isImage := strings.HasPrefix(mimeType, "image/")
	ext := strings.ToLower(filepath.Ext(header.Filename))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg":
		isImage = true
	}

	randBuf := make([]byte, 6)
	_, _ = rand.Read(randBuf)
	id := hex.EncodeToString(randBuf)

	safeName := sanitizeSegment(header.Filename)
	storedName := id + "_" + safeName

	dir := filepath.Join(s.uploadsBaseDir(), sessionID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		writeError(w, http.StatusInternalServerError, "无法创建上传目录")
		return
	}

	destPath := filepath.Join(dir, storedName)
	dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法保存文件")
		return
	}

	writtenHead, err := dst.Write(head)
	if err != nil {
		_ = dst.Close()
		_ = os.Remove(destPath)
		writeError(w, http.StatusInternalServerError, "写入文件失败")
		return
	}
	writtenRest, err := io.Copy(dst, io.LimitReader(file, maxUploadSize))
	_ = dst.Close()
	if err != nil {
		_ = os.Remove(destPath)
		writeError(w, http.StatusInternalServerError, "写入文件失败")
		return
	}

	att := session.Attachment{
		ID:       id,
		Name:     header.Filename,
		Path:     destPath,
		URL:      "/api/uploads/" + sessionID + "/" + storedName,
		MimeType: mimeType,
		Size:     int64(writtenHead) + writtenRest,
		IsImage:  isImage,
	}
	writeJSON(w, http.StatusOK, att)
}

func (s *Server) handleServeUpload(w http.ResponseWriter, r *http.Request) {
	sessionID := sanitizeSegment(r.PathValue("sessionId"))
	filename := sanitizeSegment(r.PathValue("filename"))
	fullPath := filepath.Join(s.uploadsBaseDir(), sessionID, filename)

	// Ensure path stays strictly inside uploadsBaseDir
	rel, err := filepath.Rel(s.uploadsBaseDir(), fullPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		writeError(w, http.StatusBadRequest, "无效路径")
		return
	}

	if _, err := os.Stat(fullPath); err != nil {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	http.ServeFile(w, r, fullPath)
}
