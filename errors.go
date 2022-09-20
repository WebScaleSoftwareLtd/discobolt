package discobolt

import "errors"

// RouteNotFound is used to define the error returned when a route is not found.
var RouteNotFound = errors.New("route not found")

// BadRequest is the error type thrown when a bad request is made. It wraps the origin error as to why.
type BadRequest struct {
	Err error
}

// Unwrap returns the underlying error.
func (b BadRequest) Unwrap() error {
	return b.Err
}

// Error returns the error message.
func (b BadRequest) Error() string {
	return b.Err.Error()
}
