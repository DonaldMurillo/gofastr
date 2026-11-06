package claimaudit

import "context"

var claimHandlers = map[string]func(context.Context, *Queue){
	"unsafe": func(ctx context.Context, queue *Queue) {
		job, _ := queue.dequeue(ctx, "")
		_ = queue.release(ctx, job) // want `release hands back the claim taken on line 7 on the context the claim ran under`
	},
	"safe": func(ctx context.Context, queue *Queue) {
		job, _ := queue.dequeue(ctx, "")
		settle, cancel := settleContext(ctx)
		defer cancel()
		_ = queue.release(settle, job)
	},
}

var claimHandlerSlice = []func(context.Context, *Queue){
	func(ctx context.Context, queue *Queue) {
		job, _ := queue.dequeue(ctx, "")
		_ = queue.release(ctx, job) // want `release hands back the claim taken on line 20 on the context the claim ran under`
	},
}
