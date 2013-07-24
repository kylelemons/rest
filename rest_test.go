package rest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/kylelemons/godebug/diff"
)

func TestRequest(t *testing.T) {
	type test struct {
		method string
		path   string
		code   int
		ctype  string
		output string
	}
	tgroups := []struct {
		desc  string
		input interface{}
		tests []test
	}{
		{
			desc: "basic map",
			input: map[string]interface{}{
				"foo": []string{
					"bar",
					"baz",
				},
			},
			tests: []test{{
				method: "GET",
				path:   "/",
				code:   http.StatusOK,
				ctype:  "application/json;charset=utf-8",
				output: `{"foo":["bar","baz"]}` + "\n",
			}, {
				method: "GET",
				path:   "/foo",
				code:   http.StatusOK,
				ctype:  "application/json;charset=utf-8",
				output: `["bar","baz"]` + "\n",
			}, {
				method: "GET",
				path:   "/foo/1",
				code:   http.StatusOK,
				ctype:  "application/json;charset=utf-8",
				output: `"baz"` + "\n",
			}},
		},
		{
			desc: "basic struct",
			input: &http.Request{
				Method: "PATCH",
				URL: &url.URL{
					Path: "/some/path",
				},
				ProtoMajor: 1,
				Header: http.Header{
					"Key": {"value"},
				},
			},
			tests: []test{{
				method: "GET",
				path:   "/ProtoMajor",
				code:   http.StatusOK,
				ctype:  "application/json;charset=utf-8",
				output: `1` + "\n",
			}, {
				method: "GET",
				path:   "/URL/Path",
				code:   http.StatusOK,
				ctype:  "application/json;charset=utf-8",
				output: `"/some/path"` + "\n",
			}, {
				method: "GET",
				path:   "/Header/Key/0",
				code:   http.StatusOK,
				ctype:  "application/json;charset=utf-8",
				output: `"value"` + "\n",
			}},
		},
	}

	for _, group := range tgroups {
		obj := NewObject(group.input)
		for idx, test := range group.tests {
			desc := fmt.Sprintf("%s: %d. %s(%q)", group.desc, idx, test.method, test.path)

			rec := httptest.NewRecorder()
			req := &http.Request{
				Method: test.method,
				URL: &url.URL{
					Path: test.path,
				},
			}
			obj.ServeHTTP(rec, req)
			if got, want := rec.Code, test.code; got != want {
				t.Errorf("%s: code = %v, want %v", desc, got, want)
			}
			if got := rec.HeaderMap.Get("Content-Length"); got == "" {
				t.Errorf("%s: no Content-Length header", desc)
			}
			if got, want := rec.HeaderMap.Get("Content-Type"), test.ctype; got != want {
				t.Errorf("%s: Content-Type = %q, want %q", desc, got, want)
			}
			if got, want := rec.Body.String(), test.output; got != want {
				t.Errorf("%s: body mismatch:\n%s", desc, diff.Diff(got, want))
			}
		}
	}
}
