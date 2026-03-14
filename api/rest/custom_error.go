package rest

type CustomMetadata struct {
	Param  string `json:"param"`
	Reason string `json:"reason"`
}

type CustomHTTPError struct {
	Code       string         `json:"code"`
	Originator string         `json:"originator"`
	Metadata   CustomMetadata `json:"metadata"`
	Message    string         `json:"message"`
}

type CustomHTTPErrorWrapper struct {
	Errors []CustomHTTPError `json:"errors"`
}

var customDefaultError = CustomHTTPError{
	Code:       "invalid-json",
	Originator: "jarvis",
	Metadata:   CustomMetadata{},
	Message:    "unknown error",
}

var customErorTable = map[string]CustomHTTPError{
	"custom-001": {
		Code:       "missing-parameter",
		Originator: "jarvis",
		Metadata: CustomMetadata{
			Param: "consumerIdentifier",
		},
		Message: "",
	},
}

// Custom error handler for jarvis APIs. The implementation simply
// looks up the error code in a table of known error payloads defined
// above, and creates a surrounding array of such structs as required
// by jarvis.
func GetBangoError(errString string) any {
	key := ExtractCustomErrorKey(errString)
	customError, ok := customErorTable[key]
	if !ok {
		customError = customDefaultError
	}
	customError.Message = RemoveCustomErrorKey(errString)
	var errorWrapper CustomHTTPErrorWrapper
	errorWrapper.Errors = append(errorWrapper.Errors, customError)
	return errorWrapper
}