package mapper

import (
	"errors"
	"reflect"
	"strings"

	"github.com/polarysfoundation/polarysdb/modules/gonum"
)

var (
	byteSliceType = reflect.TypeFor[[]byte]()
)

func ToStruct(src any, dst any) error {
	if src == nil {
		return errors.New("ToStruct: source is nil")
	}

	if m, ok := src.(map[string]any); ok {
		return MapToStruct(m, dst)
	}

	// Reflection fallback
	srcVal := reflect.ValueOf(src)
	dstVal := reflect.ValueOf(dst)

	if dstVal.Kind() != reflect.Pointer {
		return errors.New("ToStruct: destination must be a pointer")
	}

	if srcVal.Kind() == reflect.Pointer {
		srcVal = srcVal.Elem()
	}

	dstElem := dstVal.Elem()

	if srcVal.Type().AssignableTo(dstElem.Type()) {
		dstElem.Set(srcVal)
		return nil
	}

	return errors.New("ToStruct: incompatible types " + srcVal.Type().String() + " to " + dstElem.Type().String())
}

func MapToStruct(m map[string]any, output any) error {

	v := reflect.ValueOf(output)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return errors.New("MapToStruct: output must be pointer to struct")
	}

	v = v.Elem()
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldValue := v.Field(i)
		if !fieldValue.CanSet() {
			continue
		}

		// Nombre desde tag JSON o nombre del campo
		key := field.Name
		if tag := field.Tag.Get("json"); tag != "" && tag != "-" {
			key = strings.Split(tag, ",")[0]
		}

		raw, exists := m[key]
		if !exists || raw == nil {
			continue
		}

		rawValue := reflect.ValueOf(raw)
		if !rawValue.IsValid() {
			continue
		}

		// Caso más simple: el tipo calza
		if rawValue.Type().AssignableTo(fieldValue.Type()) {
			fieldValue.Set(rawValue)
			continue
		}

		switch fieldValue.Kind() {

		case reflect.String:
			convertString(fieldValue, raw)

		case reflect.Bool:
			convertBool(fieldValue, raw)

		case reflect.Int,
			reflect.Int8,
			reflect.Int16,
			reflect.Int32,
			reflect.Int64:

			convertInt(fieldValue, raw)

		case reflect.Uint,
			reflect.Uint8,
			reflect.Uint16,
			reflect.Uint32,
			reflect.Uint64:

			convertUint(fieldValue, raw)

		case reflect.Float32,
			reflect.Float64:

			convertFloat(fieldValue, raw)

		case reflect.Struct:

			if subMap, ok := raw.(map[string]any); ok {
				MapToStruct(subMap, fieldValue.Addr().Interface())
			}

		case reflect.Pointer:

			if fieldValue.Type() == reflect.TypeFor[*gonum.Number]() {

				if num := parseGonumNumber(raw); num != nil {
					fieldValue.Set(reflect.ValueOf(num))
				}

				continue
			}

			if fieldValue.Type().Elem().Kind() == reflect.Struct {

				if subMap, ok := raw.(map[string]any); ok {

					newStruct := reflect.New(fieldValue.Type().Elem())

					if err := MapToStruct(subMap, newStruct.Interface()); err == nil {
						fieldValue.Set(newStruct)
					}
				}
			}

		case reflect.Slice:

			// Caso especial: []byte
			if fieldValue.Type() == byteSliceType {
				convertBytes(fieldValue, raw)
				continue
			}

			convertSliceStructSafe(fieldValue, raw)

		case reflect.Array:

			convertArray(fieldValue, raw)
		}
	}

	return nil
}

func convertSliceStructSafe(fieldValue reflect.Value, raw any) {
	rawSlice, ok := raw.([]any)
	if !ok {
		return
	}

	sliceType := fieldValue.Type()
	elemType := sliceType.Elem() // tipo del elemento
	newSlice := reflect.MakeSlice(sliceType, 0, len(rawSlice))

	for _, item := range rawSlice {
		if item == nil {
			continue
		}

		// Caso: exacto asignable (ej: []*Reserves ya correcto)
		itemVal := reflect.ValueOf(item)
		if itemVal.Type().AssignableTo(elemType) {
			newSlice = reflect.Append(newSlice, itemVal)
			continue
		}

		// Caso: viene como map → struct o *struct
		if elemType.Kind() == reflect.Struct {
			if subMap, ok := item.(map[string]any); ok {
				newElem := reflect.New(elemType)
				if err := MapToStruct(subMap, newElem.Interface()); err == nil {
					newSlice = reflect.Append(newSlice, newElem.Elem())
				}
			}
			continue
		}

		if elemType.Kind() == reflect.Pointer && elemType.Elem().Kind() == reflect.Struct {
			if subMap, ok := item.(map[string]any); ok {
				newElem := reflect.New(elemType.Elem())
				if err := MapToStruct(subMap, newElem.Interface()); err == nil {
					newSlice = reflect.Append(newSlice, newElem)
				}
			}
			continue
		}
	}

	fieldValue.Set(newSlice)
}

func convertString(fieldValue reflect.Value, raw any) {
	if s, ok := raw.(string); ok {
		fieldValue.SetString(s)
	}
}

func convertBool(fieldValue reflect.Value, raw any) {

	if b, ok := raw.(bool); ok {
		fieldValue.SetBool(b)
	}
}

// Conversores numéricos
func convertInt(fieldValue reflect.Value, raw any) {

	switch v := raw.(type) {

	case int:
		fieldValue.SetInt(int64(v))

	case int8:
		fieldValue.SetInt(int64(v))

	case int16:
		fieldValue.SetInt(int64(v))

	case int32:
		fieldValue.SetInt(int64(v))

	case int64:
		fieldValue.SetInt(v)

	case uint:
		fieldValue.SetInt(int64(v))

	case uint8:
		fieldValue.SetInt(int64(v))

	case uint16:
		fieldValue.SetInt(int64(v))

	case uint32:
		fieldValue.SetInt(int64(v))

	case uint64:
		fieldValue.SetInt(int64(v))

	case float32:
		fieldValue.SetInt(int64(v))

	case float64:
		fieldValue.SetInt(int64(v))
	}
}

func convertUint(fieldValue reflect.Value, raw any) {

	switch v := raw.(type) {

	case uint:
		fieldValue.SetUint(uint64(v))

	case uint8:
		fieldValue.SetUint(uint64(v))

	case uint16:
		fieldValue.SetUint(uint64(v))

	case uint32:
		fieldValue.SetUint(uint64(v))

	case uint64:
		fieldValue.SetUint(v)

	case int:
		fieldValue.SetUint(uint64(v))

	case int8:
		fieldValue.SetUint(uint64(v))

	case int16:
		fieldValue.SetUint(uint64(v))

	case int32:
		fieldValue.SetUint(uint64(v))

	case int64:
		fieldValue.SetUint(uint64(v))

	case float32:
		fieldValue.SetUint(uint64(v))

	case float64:
		fieldValue.SetUint(uint64(v))
	}
}

func convertFloat(fieldValue reflect.Value, raw any) {

	switch v := raw.(type) {

	case float32:
		fieldValue.SetFloat(float64(v))

	case float64:
		fieldValue.SetFloat(v)

	case int:
		fieldValue.SetFloat(float64(v))

	case int8:
		fieldValue.SetFloat(float64(v))

	case int16:
		fieldValue.SetFloat(float64(v))

	case int32:
		fieldValue.SetFloat(float64(v))

	case int64:
		fieldValue.SetFloat(float64(v))

	case uint:
		fieldValue.SetFloat(float64(v))

	case uint8:
		fieldValue.SetFloat(float64(v))

	case uint16:
		fieldValue.SetFloat(float64(v))

	case uint32:
		fieldValue.SetFloat(float64(v))

	case uint64:
		fieldValue.SetFloat(float64(v))
	}
}

func convertBytes(fieldValue reflect.Value, raw any) {

	switch v := raw.(type) {

	case []uint8:
		fieldValue.SetBytes(v)

	case string:
		fieldValue.SetBytes([]byte(v))

	case []any:
		out := make([]byte, len(v))

		for i, e := range v {

			switch n := e.(type) {

			case uint8:
				out[i] = n

			case int:
				out[i] = byte(n)

			case float64:
				out[i] = byte(n)

			default:
				return
			}
		}

		fieldValue.SetBytes(out)
	}
}

func convertArray(fieldValue reflect.Value, raw any) {

	if fieldValue.Type().Elem().Kind() != reflect.Uint8 {
		return
	}

	var src []byte

	switch v := raw.(type) {

	case []byte:
		src = v

	case string:
		src = []byte(v)

	case []any:

		src = make([]byte, len(v))

		for i, e := range v {

			switch n := e.(type) {

			case uint8:
				src[i] = n

			case int:
				src[i] = byte(n)

			case int64:
				src[i] = byte(n)

			case float64:
				src[i] = byte(n)

			default:
				return
			}
		}

	default:
		return
	}

	if len(src) != fieldValue.Len() {
		return
	}

	reflect.Copy(fieldValue.Slice(0, fieldValue.Len()), reflect.ValueOf(src))
}

func parseGonumNumber(raw any) *gonum.Number {
	switch v := raw.(type) {
	case string:
		return gonum.New(v)
	case float64:
		return gonum.FromFloat64(v, gonum.DefaultPrecision)
	case float32:
		return gonum.FromFloat64(float64(v), gonum.DefaultPrecision)
	case int:
		return gonum.FromInt(v)
	case int64:
		return gonum.FromInt64(v)
	case uint64:
		return gonum.FromUint64(v)
	case map[string]any:
		if value, ok := v["value"].(string); ok {
			return gonum.New(value)
		}
	}

	return nil
}
