package agentworkspace

import "context"

// LiveAnswers is a trusted startup dependency for the original Now Task.
// Client fields cannot select a provider, approve egress or change a budget.
// A Reply contains an already-persisted native result, never a proposed write.
type LiveAnswers interface {
	Eligible(context.Context, [32]byte, Task) bool
	Execute(context.Context, [32]byte, Task) (LiveReply, error)
}

type LiveReply interface {
	Task() Task
	Answer(string) (*SourcedAnswer, error)
	Revalidate(context.Context) error
}
