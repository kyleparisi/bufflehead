package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSQLEndpoint_NoExecutor(t *testing.T) {
	s := New(0)
	handler := buildMux(s)

	body := `{"sql":"SELECT 1"}`
	req := httptest.NewRequest("POST", "/sql", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 500 {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	var result SQLResult
	json.NewDecoder(w.Body).Decode(&result)
	if result.Error == "" {
		t.Fatal("expected error in response")
	}
}

func TestSQLEndpoint_ConnectionNotFound(t *testing.T) {
	s := New(0)
	s.SetSQLExecutor(func(ctx context.Context, connName, sql string, limit int) (*SQLResult, error) {
		return nil, fmt.Errorf("connection %q not found", connName)
	})
	handler := buildMux(s)

	body := `{"sql":"SELECT 1","connection":"nonexistent"}`
	req := httptest.NewRequest("POST", "/sql", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var result SQLResult
	json.NewDecoder(w.Body).Decode(&result)
	if !strings.Contains(result.Error, "not found") {
		t.Errorf("expected error to contain 'not found', got %q", result.Error)
	}
	if !strings.Contains(result.Error, "nonexistent") {
		t.Errorf("expected error to mention connection name, got %q", result.Error)
	}
}

func TestSQLEndpoint_EmptySQL(t *testing.T) {
	s := New(0)
	s.SetSQLExecutor(func(ctx context.Context, connName, sql string, limit int) (*SQLResult, error) {
		return nil, nil
	})
	handler := buildMux(s)

	body := `{"sql":""}`
	req := httptest.NewRequest("POST", "/sql", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var result SQLResult
	json.NewDecoder(w.Body).Decode(&result)
	if !strings.Contains(result.Error, "sql is required") {
		t.Errorf("expected 'sql is required' error, got %q", result.Error)
	}
}

func TestSQLEndpoint_BadJSON(t *testing.T) {
	s := New(0)
	s.SetSQLExecutor(func(ctx context.Context, connName, sql string, limit int) (*SQLResult, error) {
		return nil, nil
	})
	handler := buildMux(s)

	req := httptest.NewRequest("POST", "/sql", strings.NewReader("{invalid"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestSQLEndpoint_Success(t *testing.T) {
	s := New(0)
	s.SetSQLExecutor(func(ctx context.Context, connName, sql string, limit int) (*SQLResult, error) {
		return &SQLResult{
			Columns: []string{"id"},
			Rows:    [][]string{{"1"}},
			Total:   1,
		}, nil
	})
	handler := buildMux(s)

	body := `{"sql":"SELECT 1 AS id"}`
	req := httptest.NewRequest("POST", "/sql", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result SQLResult
	json.NewDecoder(w.Body).Decode(&result)
	if result.Error != "" {
		t.Errorf("unexpected error: %s", result.Error)
	}
	if len(result.Columns) != 1 || result.Columns[0] != "id" {
		t.Errorf("unexpected columns: %v", result.Columns)
	}
	if result.Total != 1 {
		t.Errorf("expected total 1, got %d", result.Total)
	}
}

func TestSQLEndpoint_DefaultLimit(t *testing.T) {
	var capturedLimit int
	s := New(0)
	s.SetSQLExecutor(func(ctx context.Context, connName, sql string, limit int) (*SQLResult, error) {
		capturedLimit = limit
		return &SQLResult{Columns: []string{}, Rows: [][]string{}, Total: 0}, nil
	})
	handler := buildMux(s)

	body := `{"sql":"SELECT 1"}`
	req := httptest.NewRequest("POST", "/sql", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if capturedLimit != 100 {
		t.Errorf("expected default limit 100, got %d", capturedLimit)
	}
}

func TestS3GetObject_NoExecutor(t *testing.T) {
	s := New(0)
	handler := buildMux(s)

	body := `{"bucket":"my-bucket","key":"my-key"}`
	req := httptest.NewRequest("POST", "/s3/get-object", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 500 {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	var result S3GetObjectResult
	json.NewDecoder(w.Body).Decode(&result)
	if result.Error == "" {
		t.Fatal("expected error in response")
	}
}

func TestS3GetObject_MissingBucket(t *testing.T) {
	s := New(0)
	s.SetS3Executor(func(req S3GetObjectRequest) (*S3GetObjectResult, error) {
		return nil, nil
	})
	handler := buildMux(s)

	body := `{"key":"my-key"}`
	req := httptest.NewRequest("POST", "/s3/get-object", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var result S3GetObjectResult
	json.NewDecoder(w.Body).Decode(&result)
	if !strings.Contains(result.Error, "bucket is required") {
		t.Errorf("expected 'bucket is required' error, got %q", result.Error)
	}
}

func TestS3GetObject_MissingKey(t *testing.T) {
	s := New(0)
	s.SetS3Executor(func(req S3GetObjectRequest) (*S3GetObjectResult, error) {
		return nil, nil
	})
	handler := buildMux(s)

	body := `{"bucket":"my-bucket"}`
	req := httptest.NewRequest("POST", "/s3/get-object", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var result S3GetObjectResult
	json.NewDecoder(w.Body).Decode(&result)
	if !strings.Contains(result.Error, "key is required") {
		t.Errorf("expected 'key is required' error, got %q", result.Error)
	}
}

func TestS3GetObject_BadJSON(t *testing.T) {
	s := New(0)
	s.SetS3Executor(func(req S3GetObjectRequest) (*S3GetObjectResult, error) {
		return nil, nil
	})
	handler := buildMux(s)

	req := httptest.NewRequest("POST", "/s3/get-object", strings.NewReader("{invalid"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestS3GetObject_Success(t *testing.T) {
	s := New(0)
	s.SetS3Executor(func(req S3GetObjectRequest) (*S3GetObjectResult, error) {
		if req.Bucket != "test-bucket" || req.Key != "test-key" {
			return nil, fmt.Errorf("unexpected bucket/key: %s/%s", req.Bucket, req.Key)
		}
		return &S3GetObjectResult{
			Content:     `{"hello":"world"}`,
			ContentType: "application/json",
			Size:        17,
		}, nil
	})
	handler := buildMux(s)

	body := `{"bucket":"test-bucket","key":"test-key"}`
	req := httptest.NewRequest("POST", "/s3/get-object", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result S3GetObjectResult
	json.NewDecoder(w.Body).Decode(&result)
	if result.Error != "" {
		t.Errorf("unexpected error: %s", result.Error)
	}
	if result.Content != `{"hello":"world"}` {
		t.Errorf("unexpected content: %s", result.Content)
	}
	if result.ContentType != "application/json" {
		t.Errorf("unexpected content type: %s", result.ContentType)
	}
	if result.Size != 17 {
		t.Errorf("expected size 17, got %d", result.Size)
	}
}

func TestS3GetObject_ExecutorError(t *testing.T) {
	s := New(0)
	s.SetS3Executor(func(req S3GetObjectRequest) (*S3GetObjectResult, error) {
		return nil, fmt.Errorf("access denied")
	})
	handler := buildMux(s)

	body := `{"bucket":"my-bucket","key":"my-key"}`
	req := httptest.NewRequest("POST", "/s3/get-object", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var result S3GetObjectResult
	json.NewDecoder(w.Body).Decode(&result)
	if !strings.Contains(result.Error, "access denied") {
		t.Errorf("expected 'access denied' error, got %q", result.Error)
	}
}

func TestS3GetObject_RegionPassthrough(t *testing.T) {
	var capturedReq S3GetObjectRequest
	s := New(0)
	s.SetS3Executor(func(req S3GetObjectRequest) (*S3GetObjectResult, error) {
		capturedReq = req
		return &S3GetObjectResult{Content: "ok", Size: 2}, nil
	})
	handler := buildMux(s)

	body := `{"bucket":"b","key":"k","region":"eu-west-1","connection":"prod"}`
	req := httptest.NewRequest("POST", "/s3/get-object", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if capturedReq.Region != "eu-west-1" {
		t.Errorf("expected region eu-west-1, got %q", capturedReq.Region)
	}
	if capturedReq.Connection != "prod" {
		t.Errorf("expected connection prod, got %q", capturedReq.Connection)
	}
}
