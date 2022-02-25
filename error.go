package itscope

import (
	"net/http"
)

var (
	ErrNotFound = &UnexpectedStatusCodeError{StatusCode: http.StatusNotFound}
)

type UnexpectedStatusCodeError struct {
	Message    string
	StatusCode int
}

func (e UnexpectedStatusCodeError) Error() string {
	return e.Message
}

func (e *UnexpectedStatusCodeError) Is(tgt error) bool {
	target, ok := tgt.(*UnexpectedStatusCodeError)
	if !ok {
		return false
	}
	return e.StatusCode == target.StatusCode
}

func NewUnexpectedStatusCodeError(response *http.Response) UnexpectedStatusCodeError {
	return UnexpectedStatusCodeError{
		StatusCode: response.StatusCode,
		Message:    response.Status,
	}
}
