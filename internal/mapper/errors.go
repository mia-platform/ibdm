// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package mapper

// Ensure ParsingError implements the error interface.
var _ error = &ParsingError{}

var (
	errTemplateParsing = "mapper template parsing error"
)

type ParsingError struct {
	msg string
	err error
}

func NewParsingError(err error) *ParsingError {
	msg := errTemplateParsing
	if err != nil {
		msg = msg + "\n" + err.Error()
	}

	return &ParsingError{
		msg: msg,
		err: err,
	}
}

func (e *ParsingError) Error() string {
	return e.msg
}

func (e *ParsingError) Unwrap() error {
	return e.err
}

func (e *ParsingError) Is(target error) bool {
	if e == nil || target == nil {
		return e == target
	}

	if t, ok := target.(*ParsingError); ok {
		return e.Error() == t.Error()
	}

	return false
}
