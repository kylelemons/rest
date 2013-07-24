// Package rest implements the REST model for Representational State Transfer.
package rest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	pathpkg "path"
)

type Object struct {
	path   string
	name   string
	root   reflect.Value
	parent *Object
	child  map[string]*Object
}

func NewObject(obj interface{}) *Object {
	return newObject(nil, reflect.ValueOf(obj), nil)
}

func newObject(path []string, val reflect.Value, parent *Object) *Object {
	if len(path) > 10 {
		return nil
	}

	sub := func(id string) []string {
		return append(path, id)
	}

	obj := &Object{
		path:   "/" + strings.Join(path, "/"),
		root:   val,
		parent: parent,
		child:  map[string]*Object{},
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

	typ, kind := val.Type(), val.Kind()
	switch kind {
	case reflect.Ptr, reflect.Interface:
		if val.IsNil() {
			break
		}
		return newObject(path, val.Elem(), obj)
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.PkgPath != "" {
				continue // skip unexported fields
			}
			obj.child[field.Name] = newObject(sub(field.Name), val.Field(i), obj)
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
			obj.child[key] = newObject(sub(key), item, obj)
		}
	case reflect.Array, reflect.Slice:
		for i := 0; i < val.Len(); i++ {
			item := val.Index(i)
			key := fmt.Sprintf("%d", i)
			obj.child[key] = newObject(sub(key), item, obj)
		}
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		panic(fmt.Sprintf("can't handle %s in object at %s", kind, obj.path))
	}
	return obj
}

func Handle(path string, obj *Object) {
	path = pathpkg.Clean(path)
	http.Handle(path+"/", http.StripPrefix(path, obj))
}

func (obj *Object) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	pieces := strings.Split(r.URL.Path, "/")
	for i := 0; i < len(pieces); i++ {
		name := pieces[i]
		if name == "" && (i == 0 || i == len(pieces)-1) {
			continue
		}
		sub, ok := obj.child[name]
		if !ok {
			// TODO(kevlar): index
			http.NotFound(w, r)
			return
		}
		obj = sub
	}

	var f func(io.Writer, http.Header, *http.Request) (int, error)
	switch r.Method {
	case "GET":
		f = obj.Get
	case "POST":
		f = obj.Post
	case "PUT":
		f = obj.Put
	case "DELETE":
		f = obj.Delete
	case "PATCH":
		f = obj.Patch
	case "HEAD":
		f = obj.Head
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
	const ContentType = "application/json;charset=utf-8"
	defer func() {
		if r := recover(); r != nil {
			code, err = http.StatusInternalServerError, fmt.Errorf("encode %s: %v", v.Type().Name, r)
		}
	}()
	headers.Set("Content-Type", ContentType)
	return http.StatusOK, json.NewEncoder(w).Encode(v.Interface())
}

func (obj *Object) Get(w io.Writer, headers http.Header, r *http.Request) (int, error) {
	return encodeJSON(w, headers, obj.root)
}

func (obj *Object) Post(w io.Writer, headers http.Header, r *http.Request) (int, error) {
	return http.StatusNotImplemented, nil
}

func (obj *Object) Put(w io.Writer, headers http.Header, r *http.Request) (int, error) {
	return http.StatusNotImplemented, nil
}

func (obj *Object) Delete(w io.Writer, headers http.Header, r *http.Request) (int, error) {
	return http.StatusNotImplemented, nil
}

func (obj *Object) Patch(w io.Writer, headers http.Header, r *http.Request) (int, error) {
	return http.StatusNotImplemented, nil
}

func (obj *Object) Head(w io.Writer, headers http.Header, r *http.Request) (int, error) {
	return http.StatusNotImplemented, nil
}
