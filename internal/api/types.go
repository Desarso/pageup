package api

import "time"

type ErrorResponse struct {
	Error string `json:"error"`
}

type UploadResponse struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Created  bool   `json:"created"`
	Updated  bool   `json:"updated"`
	Revision uint64 `json:"revision"`
}

type FileResponse struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	Created     bool   `json:"created"`
	Updated     bool   `json:"updated"`
	Deleted     bool   `json:"deleted,omitempty"`
	Revision    uint64 `json:"revision"`
}

type Key struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	PublicKey string    `json:"public_key,omitempty"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type AddKeyRequest struct {
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
	Role      string `json:"role"`
}

type KeyListResponse struct {
	Keys []Key `json:"keys"`
}

type WhoAmIResponse struct {
	Key Key `json:"key"`
}
