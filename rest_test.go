// Copyright 2013 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package rest

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/kylelemons/godebug/diff"
)

func TestRequest(t *testing.T) {
	type test struct {
		method string
		path   string
		body   string
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
			}, {
				method: "GET",
				path:   "/foo/index",
				code:   http.StatusNotFound,
				ctype:  PlainText,
				output: "/foo/0\n/foo/1\n",
			}, {
				method: "POST",
				path:   "/foo",
				body:   `{"k":["v0","v1"]}`,
				code:   http.StatusNoContent,
			}, {
				method: "POST",
				path:   "/foo/k/0",
				body:   `"v2"`,
				code:   http.StatusNoContent,
			}, {
				method: "GET",
				path:   "/foo",
				code:   http.StatusOK,
				ctype:  "application/json;charset=utf-8",
				output: `{"k":["v2","v1"]}` + "\n",
			}, {
				method: "PUT",
				path:   "/foo/k",
				body:   `"v3"`,
				code:   http.StatusCreated,
				ctype:  PlainText,
				output: `/foo/k/2` + "\n",
			}, {
				method: "GET",
				path:   "/foo",
				code:   http.StatusOK,
				ctype:  "application/json;charset=utf-8",
				output: `{"k":["v2","v1","v3"]}` + "\n",
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
				ctype:  ApplicationJSON,
				output: `1` + "\n",
			}, {
				method: "GET",
				path:   "/URL/Path",
				code:   http.StatusOK,
				ctype:  ApplicationJSON,
				output: `"/some/path"` + "\n",
			}, {
				method: "GET",
				path:   "/URL/*",
				code:   http.StatusNotFound,
				ctype:  PlainText,
				output: `/URL/Fragment
/URL/Host
/URL/Opaque
/URL/Path
/URL/RawQuery
/URL/Scheme
/URL/User` + "\n",
			}, {
				method: "GET",
				path:   "/Header/Key/0",
				code:   http.StatusOK,
				ctype:  ApplicationJSON,
				output: `"value"` + "\n",
			}, {
				method: "PUT",
				path:   "/TransferEncoding",
				body:   `"identity"`,
				code:   http.StatusCreated,
				ctype:  PlainText,
				output: `/TransferEncoding/0` + "\n",
			}, {
				method: "GET",
				path:   "/TransferEncoding",
				code:   http.StatusOK,
				ctype:  ApplicationJSON,
				output: `["identity"]` + "\n",
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
				Body: ioutil.NopCloser(strings.NewReader(test.body)),
			}
			obj.ServeHTTP(rec, req)
			if got, want := rec.Code, test.code; got != want {
				t.Errorf("%s: code = %v, want %v", desc, got, want)
			}
			if got := rec.HeaderMap.Get("Content-Length"); got == "" {
				switch rec.Code {
				case http.StatusNotFound:
					// we don't care for errors
				default:
					t.Errorf("%s: no Content-Length header after %d %s", desc, rec.Code, http.StatusText(rec.Code))
				}
			}
			if got, want := rec.HeaderMap.Get("Content-Type"), test.ctype; got != want {
				t.Errorf("%s: Content-Type = %q, want %q", desc, got, want)
			}
			if got, want := rec.Body.String(), test.output; got != want {
				t.Errorf("%s: body mismatch:\n%s", desc, diff.Diff(got, want))
			}
		}

		// Print out the events we got
		old, events := obj.ESource.Tee(0)
		obj.ESource.Close()
		for _, event := range old {
			t.Logf("%s: event: %+v", group.desc, event)
		}
		for event := range events {
			t.Logf("%s: event: %+v", group.desc, event)
		}
		// TODO(kevlar): test these?
	}
}
