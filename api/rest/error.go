// Package rest implements utility functions related to surfacing out gRPC endpoints as ReST.
// We currently do this with grpc-gateway.
// For example, we require custom ReST error handling for some clients. That functionality is
// implemented here.
package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	//"log/slog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The error.go module allows implmementaiton of custom error replies for ReST replies
// processed through the grpc-gateway.
// See:
//  - https://grpc-ecosystem.github.io/grpc-gateway/docs/mapping/customizing_your_gateway/
//  - https://stackoverflow.com/questions/78377372/go-grpc-gateway-how-to-chain-multiple-http-errors
//  - Google: "Go example showing how to use a custom error handler with grpc-gateway"
//
// In the grpc-server main (actually server.go), we specify the custom HTTP error handler
// below, which typcally calls the default grpc-gateway error handler, resulting in the
// standard grpc-gateway ReST error (i.e. translating grpc error codes to the equivalent
// HTTP error codes, and using the message string from the grpc error).


const CustomErrPrefix = "[custom-"
const CustomErrPattern = "[custom-XXX]"

var customErrorFunctionTable = map[string](func(string) any){
	"custom-001": GetBangoError,
}

// CustomErrorHandler handles errors as described above
func CustomErrorHandler(ctx context.Context, mux *runtime.ServeMux, marshaler runtime.Marshaler, w http.ResponseWriter, req *http.Request, err error) {
	errString := err.Error()
	idx := strings.Index(errString, CustomErrPrefix)
	if idx == -1 {
		runtime.DefaultHTTPErrorHandler(ctx, mux, marshaler, w, req, err)
		return
	}
	//log.Logger.WithContext()
	//log.WithContext(ctx).Infof("executing custom grpc-gateway error handler with error: %s ***", errString)

	key := ExtractCustomErrorKey(errString)
	f, ok := customErrorFunctionTable[key]
	if !ok {
		//log.WithContext(ctx).Infof("using default grpc-gateway error handler because there is no function to handle %s", key)
		runtime.DefaultHTTPErrorHandler(ctx, mux, marshaler, w, req, err)
		return
	}
	s, ok := status.FromError(err)

	if !ok {
		s = status.New(codes.Internal, err.Error())
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(runtime.HTTPStatusFromCode(s.Code()))

	if err := json.NewEncoder(w).Encode(f(errString)); err != nil {
		//log.Printf("Failed to marshal error response: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func ExtractCustomErrorKey(errString string) string {
	var k string = ""
	idx := strings.Index(errString, CustomErrPrefix)
	if idx != -1 {
		k = errString[idx+1 : idx+len(CustomErrPattern)-1]
	}
	return k
}

func RemoveCustomErrorKey(errString string) string {
	idx := strings.Index(errString, CustomErrPrefix)
	if idx == -1 {
		return errString
	}
	return errString[:idx] + errString[idx+len(CustomErrPattern):]
}
