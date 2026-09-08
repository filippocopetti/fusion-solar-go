package fusionsolar

import "fmt"

type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

type AuthenticationError struct{ Message string }

func (e *AuthenticationError) Error() string { return e.Message }

type CaptchaRequiredError struct{ Message string }

func (e *CaptchaRequiredError) Error() string { return e.Message }
func authErr(s string) error                  { return &AuthenticationError{s} }
func captchaErr(s string) error               { return &CaptchaRequiredError{s} }
func fusionErr(s string) error                { return &Error{s} }
func httpErr(code int, body string) error {
	return fusionErr(fmt.Sprintf("FusionSolar HTTP %d: %s", code, body))
}
