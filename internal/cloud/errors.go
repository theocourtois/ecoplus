package cloud

import "errors"

type Kind int

const (
	KindNotFound Kind = iota + 1
	KindConflict
	KindRateLimited
	KindUnavailable
	KindInvalid
)

type Error struct {
	Kind Kind
	Err  error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return "cloud error"
	}
	return e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

func Wrap(kind Kind, err error) *Error { return &Error{Kind: kind, Err: err} }

func KindOf(err error) Kind {
	var ce *Error
	if errors.As(err, &ce) {
		return ce.Kind
	}
	return 0
}

func IsNotFound(err error) bool { return KindOf(err) == KindNotFound }

func IsConflict(err error) bool { return KindOf(err) == KindConflict }

func IsTransient(err error) bool {
	switch KindOf(err) {
	case KindRateLimited, KindUnavailable:
		return true
	}
	return err != nil && KindOf(err) == 0
}
