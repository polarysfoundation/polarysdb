package encoding

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Prefijo de tipo para desambiguar durante la deserialización.
// Sin esto, es imposible distinguir []byte("123") de string("123") de int(123).
const (
	tagNil    byte = 0
	tagBytes  byte = 1
	tagString byte = 2
	tagInt    byte = 3
	tagUint   byte = 4
	tagFloat  byte = 5
	tagBool   byte = 6
	tagJSON   byte = 7
)

func SerializeValue(value any) ([]byte, error) {
	switch v := value.(type) {
	case nil:
		return []byte{tagNil}, nil

	case []byte:
		// Pre-asignar para evitar doble alloc
		buf := make([]byte, 1, 1+len(v))
		buf[0] = tagBytes
		buf = append(buf, v...)
		return buf, nil

	case string:
		buf := make([]byte, 1, 1+len(v))
		buf[0] = tagString
		buf = append(buf, v...)
		return buf, nil

	case int:
		buf := make([]byte, 1, 21) // 1 tag + hasta 20 dígitos de int64
		buf[0] = tagInt
		return strconv.AppendInt(buf, int64(v), 10), nil
	case int8:
		buf := make([]byte, 1, 5)
		buf[0] = tagInt
		return strconv.AppendInt(buf, int64(v), 10), nil
	case int16:
		buf := make([]byte, 1, 7)
		buf[0] = tagInt
		return strconv.AppendInt(buf, int64(v), 10), nil
	case int32:
		buf := make([]byte, 1, 12)
		buf[0] = tagInt
		return strconv.AppendInt(buf, int64(v), 10), nil
	case int64:
		buf := make([]byte, 1, 21)
		buf[0] = tagInt
		return strconv.AppendInt(buf, v, 10), nil

	case uint:
		buf := make([]byte, 1, 21)
		buf[0] = tagUint
		return strconv.AppendUint(buf, uint64(v), 10), nil
	case uint8:
		buf := make([]byte, 1, 4)
		buf[0] = tagUint
		return strconv.AppendUint(buf, uint64(v), 10), nil
	case uint16:
		buf := make([]byte, 1, 6)
		buf[0] = tagUint
		return strconv.AppendUint(buf, uint64(v), 10), nil
	case uint32:
		buf := make([]byte, 1, 11)
		buf[0] = tagUint
		return strconv.AppendUint(buf, uint64(v), 10), nil
	case uint64:
		buf := make([]byte, 1, 21)
		buf[0] = tagUint
		return strconv.AppendUint(buf, v, 10), nil

	case float32:
		buf := make([]byte, 1, 32)
		buf[0] = tagFloat
		return strconv.AppendFloat(buf, float64(v), 'f', -1, 32), nil
	case float64:
		buf := make([]byte, 1, 32)
		buf[0] = tagFloat
		return strconv.AppendFloat(buf, v, 'f', -1, 64), nil

	case bool:
		if v {
			return []byte{tagBool, 1}, nil
		}
		return []byte{tagBool, 0}, nil

	default:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal complex value: %w", err)
		}
		buf := make([]byte, 1, 1+len(data))
		buf[0] = tagJSON
		buf = append(buf, data...)
		return buf, nil
	}
}

func DeserializeValue(data []byte) (any, error) {
	if len(data) == 0 {
		return nil, nil
	}

	tag := data[0]
	payload := data[1:]

	switch tag {
	case tagNil:
		return nil, nil

	case tagBytes:
		// Copiar para evitar que el caller modifique el slice original
		cp := make([]byte, len(payload))
		copy(cp, payload)
		return cp, nil

	case tagString:
		return string(payload), nil

	case tagInt:
		v, err := strconv.ParseInt(string(payload), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse int: %w", err)
		}
		return v, nil

	case tagUint:
		v, err := strconv.ParseUint(string(payload), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse uint: %w", err)
		}
		return v, nil

	case tagFloat:
		v, err := strconv.ParseFloat(string(payload), 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse float: %w", err)
		}
		return v, nil

	case tagBool:
		if len(payload) == 0 {
			return nil, fmt.Errorf("invalid bool: empty payload")
		}
		return payload[0] == 1, nil

	case tagJSON:
		var result any
		if err := json.Unmarshal(payload, &result); err != nil {
			return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
		}
		return result, nil

	default:
		return nil, fmt.Errorf("unknown type tag: %d", tag)
	}
}
