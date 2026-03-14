package main

import (
	"context"
	"fmt"

	"google.golang.org/grpc"

	nlpv1 "github.com/rkrimper1/jarvis/api/pb/nlp"
)

// nlpServerAdapter wraps nlpv1.NLPServiceServer and satisfies nlpv1.NLPServiceClient.
// This lets the voice service call NLP directly in-process without a network hop.
// The only difference between the two interfaces is the variadic grpc.CallOption
// on the client side, which is safely ignored here.
type nlpServerAdapter struct {
	srv nlpv1.NLPServiceServer
}

func (a *nlpServerAdapter) ParseIntent(
	ctx context.Context,
	req *nlpv1.ParseIntentRequest,
	_ ...grpc.CallOption,
) (*nlpv1.ParseIntentResponse, error) {
	return a.srv.ParseIntent(ctx, req)
}

func (a *nlpServerAdapter) ProcessDialogueTurn(
	ctx context.Context,
	req *nlpv1.ProcessDialogueTurnRequest,
	_ ...grpc.CallOption,
) (*nlpv1.ProcessDialogueTurnResponse, error) {
	return a.srv.ProcessDialogueTurn(ctx, req)
}

func (a *nlpServerAdapter) StreamVoiceInput(
	_ context.Context,
	_ ...grpc.CallOption,
) (grpc.BidiStreamingClient[nlpv1.StreamVoiceInputRequest, nlpv1.StreamVoiceInputResponse], error) {
	// Voice does not call StreamVoiceInput on the NLP client — it has its own
	// audio pipeline. This satisfies the interface; panic if ever called.
	return nil, fmt.Errorf("StreamVoiceInput not supported via in-process adapter")
}
