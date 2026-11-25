package labels

import "fmt"

type DecodeError struct {
	Key    string
	Reason string
}

func (e *DecodeError) Error() string {
	return fmt.Sprintf("decode label %q: %s", e.Key, e.Reason)
}

func bad(key, format string, args ...any) *DecodeError {
	return &DecodeError{Key: key, Reason: fmt.Sprintf(format, args...)}
}
