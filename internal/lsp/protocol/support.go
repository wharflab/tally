package protocol

import (
	"fmt"
	"strings"

	"encoding/json/jsontext"
	"encoding/json/v2"
)

const jsonNullLiteral = "null"

// DocumentUri is an LSP document URI.
//
//nolint:staticcheck // Keep LSP spec naming for generated compatibility.
type DocumentUri string

// URI is a generic LSP URI.
type URI string

// Method is an LSP method name.
type Method string

// HasTextDocumentURI exposes a document URI for typed request params.
type HasTextDocumentURI interface {
	TextDocumentURI() DocumentUri
}

// HasTextDocumentPosition exposes both URI and position.
type HasTextDocumentPosition interface {
	HasTextDocumentURI
	TextDocumentPosition() Position
}

// HasLocations exposes bulk locations.
type HasLocations interface {
	GetLocations() *[]Location
}

// HasLocation exposes a single location.
type HasLocation interface {
	GetLocation() Location
}

func unmarshalPtrTo[T any](data []byte) (*T, error) {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("failed to unmarshal %T: %w", (*T)(nil), err)
	}
	return &v, nil
}

func unmarshalValue[T any](data []byte) (T, error) {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return *new(T), fmt.Errorf("failed to unmarshal %T: %w", (*T)(nil), err)
	}
	return v, nil
}

func unmarshalAny(data []byte) (any, error) {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("failed to unmarshal any: %w", err)
	}
	return v, nil
}

func unmarshalEmpty(data []byte) (any, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == jsonNullLiteral {
		return struct{}{}, nil
	}
	return nil, fmt.Errorf("expected empty or null, got: %s", trimmed)
}

func assertOnlyOne(message string, values ...bool) {
	count := 0
	for _, v := range values {
		if v {
			count++
		}
	}
	if count != 1 {
		panic(message)
	}
}

func assertAtMostOne(message string, values ...bool) {
	count := 0
	for _, v := range values {
		if v {
			count++
		}
	}
	if count > 1 {
		panic(message)
	}
}

// RequestInfo carries method metadata for a typed request.
type RequestInfo[Params, Resp any] struct {
	_      [0]Params
	_      [0]Resp
	Method Method
}

// UnmarshalResult converts JSON-RPC result payload to the expected response type.
func (info RequestInfo[Params, Resp]) UnmarshalResult(result any) (Resp, error) {
	if r, ok := result.(Resp); ok {
		return r, nil
	}

	raw, ok := result.(jsontext.Value)
	if !ok {
		return *new(Resp), fmt.Errorf("expected jsontext.Value, got %T", result)
	}

	r, err := unmarshalResult(info.Method, raw)
	if err != nil {
		return *new(Resp), err
	}

	typed, ok := r.(Resp)
	if !ok {
		return *new(Resp), fmt.Errorf("unexpected result type %T", r)
	}

	return typed, nil
}

// NotificationInfo carries method metadata for a typed notification.
type NotificationInfo[Params any] struct {
	_      [0]Params
	Method Method
}

// Null encodes/decodes JSON null.
type Null struct{}

func (Null) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	data, err := dec.ReadValue()
	if err != nil {
		return err
	}
	if string(data) != jsonNullLiteral {
		return fmt.Errorf("expected null, got %s", data)
	}
	return nil
}

func (Null) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.Null)
}
