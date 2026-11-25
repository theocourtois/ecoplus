package cloud

import "context"

type Priority int

const (
	High Priority = iota
	Low
)

type priorityKey struct{}

func WithPriority(ctx context.Context, p Priority) context.Context {
	return context.WithValue(ctx, priorityKey{}, p)
}

func PriorityOf(ctx context.Context) Priority {
	if p, ok := ctx.Value(priorityKey{}).(Priority); ok {
		return p
	}
	return High
}
