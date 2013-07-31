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

// Package rest implements the REST model for Representational State Transfer.
package rest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"

	pathpkg "path"

	"kylelemons.net/go/esource"
)

// Standard Content-Type values
const (
	ApplicationJSON = "application/json;charset=utf-8"
	PlainText       = "text/plain;charset=utf-8"
)

type Object struct {
	path   string
	name   string
	parent *Object
	child  map[string]*Object

	root reflect.Value
	typ  reflect.Type
	kind reflect.Kind

	rw sync.RWMutex

	ESource *esource.EventSource
}

func NewObject(obj interface{}) *Object {
	es := esource.New()
	return newObject([]string{""}, reflect.ValueOf(obj), nil, es)
}

func newObject(path []string, val reflect.Value, parent *Object, es *esource.EventSource) *Object {
	typ, kind := val.Type(), val.Kind()

	if len(path) > 10 {
		panic("DEBUG: depth limit exceeded")
	}

	sub := func(id string) []string {
		return append(path, id)
	}

	obj := &Object{
		path:    "/" + pathpkg.Join(path...),
		parent:  parent,
		child:   map[string]*Object{},
		root:    val,
		typ:     typ,
		kind:    kind,
		ESource: es,
	}
	if len(path) > 0 {
		obj.name = path[len(path)-1]
	}

	if !val.IsValid() {
		panic(fmt.Sprintf("invalid object at %s", obj.path))
	}
	if !val.CanInterface() {
		panic(fmt.Sprintf("can't call Interface on object at %s", obj.path))
	}

	switch kind {
	case reflect.Ptr, reflect.Interface:
		if val.IsNil() {
			break
		}
		sub := newObject(path, val.Elem(), obj, es)
		obj.child = sub.child
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.PkgPath != "" {
				continue // skip unexported fields
			}
			obj.child[field.Name] = newObject(sub(field.Name), val.Field(i), obj, es)
		}
	case reflect.Map:
		for _, keyVal := range val.MapKeys() {
			var key string
			switch keyVal.Kind() {
			case reflect.String:
				key = keyVal.String()
			default:
				if !keyVal.CanInterface() {
					panic(fmt.Sprintf("can't call Interface on non-string map key at %s", obj.path))
				}
				key = fmt.Sprintf("%v", keyVal.Interface())
			}
			item := val.MapIndex(keyVal)
			obj.child[key] = newObject(sub(key), item, obj, es)
		}
	case reflect.Array, reflect.Slice:
		for i := 0; i < val.Len(); i++ {
			item := val.Index(i)
			key := fmt.Sprintf("%d", i)
			obj.child[key] = newObject(sub(key), item, obj, es)
		}
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		panic(fmt.Sprintf("can't handle %s in object at %s", kind, obj.path))
	}
	return obj
}

var (
	stringType = reflect.TypeOf("")
)

func (obj *Object) set(v reflect.Value) error {
	parent := obj.parent
	if parent == nil {
		return fmt.Errorf("cannot set object with no parent")
	}

	switch parent.kind {
	case reflect.Map:
		var key reflect.Value
		switch ktyp := parent.typ.Key(); ktyp {
		case stringType:
			key = reflect.ValueOf(obj.name)
		default:
			// TODO(kevlar): technically we can convert to any type to which string is convertable
			return fmt.Errorf("cannot set key of non-string map type %s", parent.typ)
		}
		parent.root.SetMapIndex(key, v)
	default:
		if !obj.root.CanSet() {
			return fmt.Errorf("cannot set a %s", obj.typ)
		}
		obj.root.Set(v)
	}

	path := strings.Split(obj.path, "/")
	parent.child[obj.name] = newObject(path, v, parent, obj.ESource)
	return nil
}

func (obj *Object) del() error {
	parent := obj.parent
	if parent == nil {
		return fmt.Errorf("cannot delete object with no parent")
	}

	switch parent.kind {
	default:
		return fmt.Errorf("cannot delete children of a %s", parent.kind)
	}
	return nil
}

func Handle(path string, obj *Object) {
	path = pathpkg.Clean(path)
	http.Handle(path+"/", http.StripPrefix(path, obj))
}

func (obj *Object) find(pieces []string) (*Object, bool) {
	// If there are no pieces left, we're done
	if len(pieces) == 0 {
		return obj, true
	}

	// If there is a // in the path or a / at the end, ignore it
	if pieces[0] == "" {
		return obj.find(pieces[1:])
	}

	// Find a child if we have one
	obj.rw.RLock()
	ret, ok := obj.child[pieces[0]]
	obj.rw.RUnlock()
	if !ok {
		return obj, false
	}

	return ret.find(pieces[1:])
}

func (obj *Object) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	pieces := strings.Split(r.URL.Path, "/")[1:]
	actual, found := obj.find(pieces)
	if !found {
		obj.rw.RLock()
		defer obj.rw.RUnlock()
		w.Header().Set("Content-Type", PlainText)
		w.WriteHeader(http.StatusNotFound)
		keys := make([]string, 0, len(obj.child))
		for key := range actual.child {
			keys = append(keys, pathpkg.Join(actual.path, key))
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintln(w, key)
		}
		return
	}
	obj = actual

	var f func(io.Writer, http.Header, *http.Request) (int, error)
	switch r.Method {
	case "GET":
		f = obj.Get
		obj.rw.RLock()
		defer obj.rw.RUnlock()
	case "POST":
		f = obj.Post
		obj.rw.Lock()
		defer obj.rw.Unlock()
	case "PUT":
		f = obj.Put
		obj.rw.Lock()
		defer obj.rw.Unlock()
	case "DELETE":
		f = obj.Delete
		obj.rw.Lock()
		defer obj.rw.Unlock()
	case "PATCH":
		f = obj.Patch
		obj.rw.Lock()
		defer obj.rw.Unlock()
	case "HEAD":
		f = obj.Head
		obj.rw.RLock()
		defer obj.rw.RUnlock()
	default:
		w.Header().Set("Allow", "GET, POST, PUT, DELETE, PATCH, HEAD")
		http.Error(w, r.Method+" not allowed", http.StatusMethodNotAllowed)
		return
	}

	buf := new(bytes.Buffer)
	code, err := f(buf, w.Header(), r)
	if err != nil {
		if code == 0 || code == http.StatusOK {
			code = http.StatusInternalServerError
		}
		http.Error(w, err.Error(), code)
		return
	}

	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.WriteHeader(code)
	buf.WriteTo(w)
}

func encodeJSON(w io.Writer, headers http.Header, v reflect.Value) (code int, err error) {
	defer func() {
		if r := recover(); r != nil {
			code, err = http.StatusInternalServerError, fmt.Errorf("encode %s: %v", v.Type().Name, r)
		}
	}()
	headers.Set("Content-Type", ApplicationJSON)
	return http.StatusOK, json.NewEncoder(w).Encode(v.Interface())
}

func decodeJSON(r io.Reader, typ reflect.Type) (vptr reflect.Value, err error) {
	zptr := reflect.New(typ)
	if err := json.NewDecoder(r).Decode(zptr.Interface()); err != nil {
		return reflect.Value{}, fmt.Errorf("failed to decode body as JSON: %s", err)
	}
	return zptr.Elem(), nil
}

func (obj *Object) Get(w io.Writer, headers http.Header, r *http.Request) (int, error) {
	return encodeJSON(w, headers, obj.root)
}

func (obj *Object) Post(w io.Writer, headers http.Header, r *http.Request) (int, error) {
	v, err := decodeJSON(r.Body, obj.typ)
	if err != nil {
		return http.StatusBadRequest, err
	}
	if err := obj.set(v); err != nil {
		return http.StatusBadRequest, err
	}
	obj.ESource.Events <- esource.Event{
		Type: "post",
		Data: obj.path,
	}
	return http.StatusNoContent, nil
}

func (obj *Object) Put(w io.Writer, headers http.Header, r *http.Request) (int, error) {
	root := obj.root
	for {
		k := root.Kind()
		// TODO(kevlar) this probably doesn't actually with pointers... should it?
		if k != reflect.Ptr && k != reflect.Interface {
			break
		}
		if root.IsNil() {
			break
		}
		root = root.Elem()
	}
	k, t := root.Kind(), root.Type()

	if k != reflect.Slice {
		return http.StatusBadRequest, fmt.Errorf("cannot PUT object in non-slice type %s", t)
	}
	v, err := decodeJSON(r.Body, t.Elem())
	if err != nil {
		return http.StatusBadRequest, err
	}
	path := pathpkg.Join(obj.path, strconv.Itoa(root.Len()))
	root = reflect.Append(root, v)
	if err := obj.set(root); err != nil {
		return http.StatusBadRequest, err
	}
	obj.ESource.Events <- esource.Event{
		Type: "put",
		Data: path,
	}
	headers.Set("Content-Type", PlainText)
	fmt.Fprintln(w, path)
	return http.StatusCreated, nil
}

func (obj *Object) Delete(w io.Writer, headers http.Header, r *http.Request) (int, error) {
	if err := obj.del(); err != nil {
		return http.StatusBadRequest, err
	}
	obj.ESource.Events <- esource.Event{
		Type: "delete",
		Data: obj.path,
	}
	return http.StatusNoContent, nil
}

func (obj *Object) Patch(w io.Writer, headers http.Header, r *http.Request) (int, error) {
	return http.StatusNotImplemented, nil
}

func (obj *Object) Head(w io.Writer, headers http.Header, r *http.Request) (int, error) {
	return http.StatusNotImplemented, nil
}
