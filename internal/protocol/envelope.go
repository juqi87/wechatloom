package protocol

import (
	"encoding/json"
	"io"
)

const SchemaVersion = "1.0"

// Envelope is the stable machine-readable boundary between WeChatLoom and agents.
type Envelope struct {
	Success       bool     `json:"success"`
	Code          string   `json:"code"`
	Message       string   `json:"message"`
	SchemaVersion string   `json:"schema_version"`
	Status        string   `json:"status"`
	Retryable     bool     `json:"retryable"`
	Warnings      []string `json:"warnings"`
	Data          any      `json:"data,omitempty"`
}

func OK(code, message, status string, data any) Envelope {
	return Envelope{
		Success:       true,
		Code:          code,
		Message:       message,
		SchemaVersion: SchemaVersion,
		Status:        status,
		Retryable:     false,
		Warnings:      []string{},
		Data:          data,
	}
}

func Failure(code, message, status string, retryable bool, data any) Envelope {
	return Envelope{
		Success:       false,
		Code:          code,
		Message:       message,
		SchemaVersion: SchemaVersion,
		Status:        status,
		Retryable:     retryable,
		Warnings:      []string{},
		Data:          data,
	}
}

func WriteJSON(writer io.Writer, envelope Envelope) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(envelope)
}
