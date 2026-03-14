package notify

import "context"

type Notifier interface {
	SendMessage(ctx context.Context, message string) error
	SendError(ctx context.Context, component string, err error)
	SendInfo(ctx context.Context, message string)
	SendWarning(ctx context.Context, message string)
	SendSuccess(ctx context.Context, message string)
}
