package utils

import (
	"errors"
	"mime/multipart"
	"path/filepath"
	"strings"
)

func AllowedDocument(ext, mime string) bool {
	ext = strings.ToLower(ext)
	return (ext == ".pdf" && mime == "application/pdf") ||
		(ext == ".doc" && mime == "application/msword") ||
		(ext == ".docx" && mime == "application/vnd.openxmlformats-officedocument.wordprocessingml.document") ||
		(ext == ".csv" && mime == "text/csv")
}

func AllowedVideo(ext, mime string) bool {
	ext = strings.ToLower(ext)
	return (ext == ".mp4" && mime == "video/mp4") ||
		(ext == ".mov" && mime == "video/quicktime") ||
		(ext == ".avi" && mime == "video/x-msvideo")
}

func AllowedTemplate(ext, mime string) bool {
	ext = strings.ToLower(ext)
	return (ext == ".ppt" && mime == "application/vnd.ms-powerpoint") ||
		(ext == ".pptx" && mime == "application/vnd.openxmlformats-officedocument.presentationml.presentation") ||
		(ext == ".potx" && mime == "application/vnd.openxmlformats-officedocument.presentationml.template")
}

// validate resource files

func ValidateResourceFile(fileHeader *multipart.FileHeader, resourceType string) error {
	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
	mimeType := fileHeader.Header.Get("Content-Type")

	switch resourceType {
	case "Document":
		if !AllowedDocument(ext, mimeType) {
			return errors.New("invalid document type")
		}
	case "Video":
		if !AllowedVideo(ext, mimeType) {
			return errors.New("invalid video type")
		}
	case "Template":
		if !AllowedTemplate(ext, mimeType) {
			return errors.New("invalid template type")
		}
	default:
		return errors.New("unsupported resource type for file upload")
	}

	return nil
}
