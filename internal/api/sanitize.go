package api

import (
	"reflect"
	"strings"
)

// sanitize strips what a terminal would act on rather than show from every string in v, which the API decoded into:
// control characters but newlines and tabs, and the bidirectional overrides that make text read other than it is.
// A commit's body could otherwise move the cursor, retitle the terminal or hide part of itself.
func sanitize(v any) {
	walk(reflect.ValueOf(v))
}

func walk(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			walk(v.Elem())
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				walk(v.Field(i))
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			walk(v.Index(i))
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			e := v.MapIndex(k)
			if e.Kind() == reflect.String {
				v.SetMapIndex(k, reflect.ValueOf(cleanString(e.String())).Convert(e.Type()))
			} else if e.Kind() == reflect.Pointer || e.Kind() == reflect.Interface {
				walk(e)
			}
		}
	case reflect.String:
		if v.CanSet() {
			if s := v.String(); s != cleanString(s) {
				v.SetString(cleanString(s))
			}
		}
	}
}

func cleanString(s string) string {
	if strings.IndexFunc(s, unsafeRune) < 0 {
		return s
	}

	return strings.Map(func(r rune) rune {
		if unsafeRune(r) {
			return -1
		}
		return r
	}, s)
}

func unsafeRune(r rune) bool {
	switch {
	case r == '\n' || r == '\t':
		return false
	case r < 0x20 || r == 0x7f || r >= 0x80 && r < 0xa0:
		return true
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
		return true
	}

	return false
}
