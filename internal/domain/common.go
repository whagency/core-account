package domain

import (
	"fmt"
	"strconv"
)

type ID uint64

type idInput interface {
	~uint64 | ~string
}

// NewID creates a new ID from a value of type uint64 or string.
func NewID[T idInput](v T) (ID, error) {
	switch any(v).(type) {
	case uint64:
		return NewIDFromUint64(any(v).(uint64)), nil
	case string:
		return NewIDFromString(any(v).(string))
	default:
		return 0, fmt.Errorf("[domain][common:NewID][err: unsupported ID type %T]", v)
	}
}

// NewIDFromUint64 converts a uint64 value to an ID type.
func NewIDFromUint64(id uint64) ID {
	return ID(id)
}

// NewIDFromString converts a string representation of an ID to an ID type.
func NewIDFromString(s string) (ID, error) {
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("[domain][common:NewIDFromString][err: invalid ID string: %w]", err)
	}
	return ID(v), nil
}

// NewNullableIDFromUint64 converts a pointer to a uint64 to a pointer to an ID type.
func NewNullableIDFromUint64(id *uint64) *ID {
	if id == nil {
		return nil
	}
	v := NewIDFromUint64(*id)
	return &v
}

// Uint64 converts the ID to a uint64 type.
func (id ID) Uint64() uint64 {
	return uint64(id)
}

// String converts the ID to its string representation.
func (id ID) String() string {
	return strconv.FormatUint(uint64(id), 10)
}
