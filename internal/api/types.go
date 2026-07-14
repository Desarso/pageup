package api

import "time"

type ErrorResponse struct {
	Error string `json:"error"`
}

type UploadResponse struct {
	ID      string `json:"id"`
	URL     string `json:"url"`
	Created bool   `json:"created"`
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
