package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestStatusMapping(t *testing.T) {
	cases := map[Code]int{
		CodeInvalidRequest: 400, CodeUnauthorized: 401, CodeNotFound: 404, CodeConflict: 409,
		CodeUpstream: 503, CodeRateLimited: 503, CodeNotReady: 503, CodeInternal: 500, Code("other"): 500,
	}
	for code, want := range cases {
		e := NewError(code, "x")
		if e.Status() != want {
			t.Errorf("%s: got %d want %d", code, e.Status(), want)
		}
		if e.Retryable() != (want == http.StatusServiceUnavailable) {
			t.Errorf("%s: retryable mismatch", code)
		}
	}
}

func TestErrorAs(t *testing.T) {
	var err error = NewError(CodeNotFound, "gone %d", 1)
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Message != "gone 1" || err.Error() != "not_found: gone 1" {
		t.Fatalf("unexpected %v", err)
	}
}

func TestRequestValidation(t *testing.T) {
	ok := ServerRequest{Name: "web-1", ServerType: "cx22", Location: "nbg1", Image: "ubuntu-24.04"}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	mutate := map[string]func(*ServerRequest){
		"bad name":     func(r *ServerRequest) { r.Name = "Web_1" },
		"long name":    func(r *ServerRequest) { r.Name = strings.Repeat("a", 64) },
		"dash edge":    func(r *ServerRequest) { r.Name = "-a" },
		"server type":  func(r *ServerRequest) { r.ServerType = "" },
		"location":     func(r *ServerRequest) { r.Location = "" },
		"image":        func(r *ServerRequest) { r.Image = "" },
		"user data":    func(r *ServerRequest) { r.UserData = strings.Repeat("x", MaxUserDataBytes+1) },
		"reserved key": func(r *ServerRequest) { r.Labels = map[string]string{"ecoplus/state": "x"} },
	}
	for name, m := range mutate {
		r := ok
		m(&r)
		if err := r.Validate(); err == nil || err.Code != CodeInvalidRequest {
			t.Errorf("%s: expected invalid_request, got %v", name, err)
		}
	}
	longest := ok
	longest.UserData = strings.Repeat("x", MaxUserDataBytes)
	if err := longest.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPatchValidation(t *testing.T) {
	bad, empty := "Bad_Name", ""
	good := "ok"
	reserved := map[string]string{"ecoplus/x": "y"}
	if (ServerPatch{Name: &bad}).Validate() == nil || (ServerPatch{Image: &empty}).Validate() == nil ||
		(ServerPatch{Labels: &reserved}).Validate() == nil {
		t.Fatal("expected failures")
	}
	fine := map[string]string{"a": "b"}
	if err := (ServerPatch{Name: &good, Image: &good, Labels: &fine}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (ServerPatch{}).Validate(); err != nil {
		t.Fatal(err)
	}
}
