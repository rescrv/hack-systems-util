package ubench

import (
	"flag"
	"fmt"
	"reflect"
	"time"
	"unicode"
	"unsafe"

	"hack.systems/util/assert"
)

func CommentString(params, results interface{}) string {
	s := ""
	sep := "#"
	P := reflect.TypeOf(params)
	for i := 0; i < P.NumField(); i++ {
		s += sep + P.Field(i).Name
		sep = " "
	}
	R := reflect.TypeOf(results)
	for i := 0; i < R.NumField(); i++ {
		s += sep + R.Field(i).Name
		sep = " "
	}
	return s
}

func PrintCommentString(params, results interface{}) {
	fmt.Println(CommentString(params, results))
}

func ResultString(params, results interface{}) string {
	s := ""
	first := true
	P := reflect.ValueOf(params)
	for i := 0; i < P.NumField(); i++ {
		if !first {
			s += " "
		}
		first = false
		s += fmt.Sprintf("%v", P.Field(i).Interface())
	}
	R := reflect.ValueOf(results)
	for i := 0; i < R.NumField(); i++ {
		if !first {
			s += " "
		}
		first = false
		s += fmt.Sprintf("%v", R.Field(i).Interface())
	}
	return s
}

func PrintResultString(params, results interface{}) {
	fmt.Println(ResultString(params, results))
}

func FieldNameToFlag(name string) string {
	s := ""
	runes := []rune(name)
	for i := range runes {
		if i > 0 && i+1 < len(runes) && unicode.IsUpper(runes[i]) && unicode.IsLower(runes[i+1]) {
			s += "-"
		}
		s += string(unicode.ToLower(runes[i]))
	}
	return s
}

func AddToFlagSet(f *flag.FlagSet, ptr interface{}) {
	V := reflect.ValueOf(ptr).Elem()
	T := V.Type()
	assert.True(T.NumField() == V.NumField(),
		"reflect API assumption violated")
	for i := 0; i < T.NumField(); i++ {
		f := T.Field(i)
		if len(string(f.Tag)) == 0 {
			continue
		}
		v := V.Field(i)
		switch v.Interface().(type) {
		case bool:
			p := (*bool)(unsafe.Pointer(v.Addr().Pointer()))
			flag.BoolVar(p, FieldNameToFlag(f.Name), *p, string(f.Tag))
		case time.Duration:
			p := (*time.Duration)(unsafe.Pointer(v.Addr().Pointer()))
			flag.DurationVar(p, FieldNameToFlag(f.Name), *p, string(f.Tag))
		case float64:
			p := (*float64)(unsafe.Pointer(v.Addr().Pointer()))
			flag.Float64Var(p, FieldNameToFlag(f.Name), *p, string(f.Tag))
		case int:
			p := (*int)(unsafe.Pointer(v.Addr().Pointer()))
			flag.IntVar(p, FieldNameToFlag(f.Name), *p, string(f.Tag))
		case int64:
			p := (*int64)(unsafe.Pointer(v.Addr().Pointer()))
			flag.Int64Var(p, FieldNameToFlag(f.Name), *p, string(f.Tag))
		case string:
			p := (*string)(unsafe.Pointer(v.Addr().Pointer()))
			flag.StringVar(p, FieldNameToFlag(f.Name), *p, string(f.Tag))
		case uint:
			p := (*uint)(unsafe.Pointer(v.Addr().Pointer()))
			flag.UintVar(p, FieldNameToFlag(f.Name), *p, string(f.Tag))
		case uint64:
			p := (*uint64)(unsafe.Pointer(v.Addr().Pointer()))
			flag.Uint64Var(p, FieldNameToFlag(f.Name), *p, string(f.Tag))
		default:
			panic("unknown parameter type")
		}
	}
}

func AddFlags(params interface{}) {
	AddToFlagSet(flag.CommandLine, params)
}
