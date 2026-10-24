package dict

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DecodedValue returns the item value decoded according to ValueType. It falls
// back to the raw string when the stored value is not decodable, so a bad row
// never breaks a whole settings read.
func (i *Item) DecodedValue() any {
	raw := i.EffectiveValue()
	switch i.ValueType {
	case ValueTypeNumber:
		number := json.Number(raw)
		if _, err := number.Float64(); err == nil {
			return number
		}
		return raw
	case ValueTypeBoolean:
		value, err := strconv.ParseBool(raw)
		if err == nil {
			return value
		}
		return raw
	case ValueTypeJSON:
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err == nil {
			return value
		}
		return raw
	default:
		return raw
	}
}

// encodeValue converts an arbitrary Go value into the text stored in
// dict_items.value plus the matching value_type.
func encodeValue(value any) (string, ValueType, error) {
	switch v := value.(type) {
	case string:
		return v, ValueTypeString, nil
	case bool:
		return strconv.FormatBool(v), ValueTypeBoolean, nil
	case json.Number:
		return v.String(), ValueTypeNumber, nil
	case int:
		return strconv.FormatInt(int64(v), 10), ValueTypeNumber, nil
	case int8:
		return strconv.FormatInt(int64(v), 10), ValueTypeNumber, nil
	case int16:
		return strconv.FormatInt(int64(v), 10), ValueTypeNumber, nil
	case int32:
		return strconv.FormatInt(int64(v), 10), ValueTypeNumber, nil
	case int64:
		return strconv.FormatInt(v, 10), ValueTypeNumber, nil
	case uint:
		return strconv.FormatUint(uint64(v), 10), ValueTypeNumber, nil
	case uint8:
		return strconv.FormatUint(uint64(v), 10), ValueTypeNumber, nil
	case uint16:
		return strconv.FormatUint(uint64(v), 10), ValueTypeNumber, nil
	case uint32:
		return strconv.FormatUint(uint64(v), 10), ValueTypeNumber, nil
	case uint64:
		return strconv.FormatUint(v, 10), ValueTypeNumber, nil
	case float32:
		return strconv.FormatFloat(float64(v), 'g', -1, 32), ValueTypeNumber, nil
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), ValueTypeNumber, nil
	case time.Time:
		return v.Format(time.RFC3339), ValueTypeString, nil
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return "", "", invalid(fmt.Sprintf("unsupported dict value of type %T", value))
		}
		return string(raw), ValueTypeJSON, nil
	}
}

// itemsFromObject turns an object setting into key/value items. Field order is
// sorted so repeated writes produce deterministic sort_order values.
func itemsFromObject(values map[string]any) ([]Item, error) {
	keys := make([]string, 0, len(values))
	for field := range values {
		if values[field] == nil {
			continue
		}
		keys = append(keys, field)
	}
	sort.Strings(keys)

	items := make([]Item, 0, len(keys))
	for i, field := range keys {
		key, err := normalizeItemKey(field)
		if err != nil {
			return nil, err
		}
		value, valueType, err := encodeValue(values[field])
		if err != nil {
			return nil, err
		}
		items = append(items, Item{
			Key:       key,
			Label:     key,
			Value:     value,
			ValueType: valueType,
			SortOrder: i,
			Status:    StatusEnabled,
		})
	}
	return items, nil
}

// itemsFromList turns an array setting into ordered items.
func itemsFromList(values []any) ([]Item, error) {
	items := make([]Item, 0, len(values))
	for i, raw := range values {
		if raw == nil {
			continue
		}
		item, err := itemFromValue(raw, i)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

// toAnyMap accepts any Go map with string keys (map[string]any,
// map[string]string, named map types) without forcing callers to convert.
func toAnyMap(value any) (map[string]any, bool) {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() || rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return nil, false
	}
	out := make(map[string]any, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		out[iter.Key().String()] = iter.Value().Interface()
	}
	return out, true
}

// toAnySlice accepts any slice or array ([]any, []string, named slice types).
func toAnySlice(value any) ([]any, bool) {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
		return nil, false
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

// itemFromValue maps one element of an array setting to an item. Objects may
// opt into explicit fields (key/label/value/value_type/sort/status/remark);
// otherwise the object is stored as a JSON value.
func itemFromValue(raw any, index int) (*Item, error) {
	object, isObject := asObject(raw)
	if !isObject {
		value, valueType, err := encodeValue(raw)
		if err != nil {
			return nil, err
		}
		key, err := normalizeItemKey(value)
		if err != nil {
			// A very long primitive still gets a stable synthetic key so the
			// value itself is not lost.
			key = strconv.Itoa(index + 1)
		}
		return &Item{
			Key:       key,
			Label:     value,
			Value:     value,
			ValueType: valueType,
			SortOrder: index,
			Status:    StatusEnabled,
		}, nil
	}

	explicitKey, hasKey := object["key"].(string)
	if !hasKey {
		value, valueType, err := encodeValue(object)
		if err != nil {
			return nil, err
		}
		key := strconv.Itoa(index + 1)
		return &Item{
			Key:       key,
			Label:     key,
			Value:     value,
			ValueType: valueType,
			SortOrder: index,
			Status:    StatusEnabled,
		}, nil
	}

	key, err := normalizeItemKey(explicitKey)
	if err != nil {
		return nil, err
	}
	label := key
	if rawLabel, ok := object["label"].(string); ok && strings.TrimSpace(rawLabel) != "" {
		label = strings.TrimSpace(rawLabel)
	}

	value := ""
	valueType := ValueTypeString
	if rawValue, ok := object["value"]; ok && rawValue != nil {
		value, valueType, err = encodeValue(rawValue)
		if err != nil {
			return nil, err
		}
	}

	if rawType, ok := object["value_type"].(string); ok && rawType != "" {
		valueType = ValueType(rawType)
		if !valueType.Valid() {
			return nil, invalid("invalid item value_type")
		}
	}

	sortOrder := index
	if rawSort, ok := asInt(object["sort"]); ok {
		sortOrder = rawSort
	}
	status := StatusEnabled
	if rawStatus, ok := asInt(object["status"]); ok {
		status = Status(rawStatus)
	}
	remark := ""
	if rawRemark, ok := object["remark"].(string); ok {
		remark = strings.TrimSpace(rawRemark)
	}

	return &Item{
		Key:       key,
		Label:     label,
		Value:     value,
		ValueType: valueType,
		SortOrder: sortOrder,
		Status:    status,
		Remark:    remark,
	}, nil
}

func asObject(value any) (map[string]any, bool) {
	switch v := value.(type) {
	case map[string]any:
		return v, true
	case map[string]string:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = item
		}
		return out, true
	default:
		return nil, false
	}
}

func asInt(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		return int(v), true
	case float32:
		return int(v), true
	case int:
		return v, true
	case int8:
		return int(v), true
	case int16:
		return int(v), true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case uint:
		return int(v), true
	case uint64:
		return int(v), true
	case json.Number:
		parsed, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(parsed), true
	default:
		return 0, false
	}
}

type destinationKind int

const (
	destinationOther destinationKind = iota
	destinationMap
	destinationSlice
)

func pointerKind(dst any) (destinationKind, error) {
	value := reflect.ValueOf(dst)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return destinationOther, invalid("destination must be a non-nil pointer")
	}
	switch value.Elem().Kind() {
	case reflect.Map:
		return destinationMap, nil
	case reflect.Slice, reflect.Array:
		return destinationSlice, nil
	default:
		return destinationOther, nil
	}
}

func decodeJSON(source, dst any) error {
	raw, err := json.Marshal(source)
	if err != nil {
		return invalid("dict value cannot be encoded as JSON")
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return invalid("destination does not match the dict value shape")
	}
	return nil
}
