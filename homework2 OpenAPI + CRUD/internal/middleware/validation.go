package middleware

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/mail"
	"strings"

	"github.com/google/uuid"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"marketplace/internal/api"
	"marketplace/internal/apperr"
)

func Validation(spec *openapi3.T) (api.MiddlewareFunc, error) {
	defineFormats()
	spec.Servers = nil
	router, err := gorillamux.NewRouter(spec)
	if err != nil {
		return nil, err
	}
	options := &openapi3filter.Options{
		MultiError:         true,
		AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
		SchemaValidationOptions: []openapi3.SchemaValidationOption{
			openapi3.EnableFormatValidation(),
		},
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route, pathParams, err := router.FindRoute(r)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			input := &openapi3filter.RequestValidationInput{
				Request:    r,
				PathParams: pathParams,
				Route:      route,
				Options:    options,
			}
			if err := openapi3filter.ValidateRequest(r.Context(), input); err != nil {
				apperr.Write(w, apperr.Validation("request validation failed", map[string]any{"fields": collectViolations(err)}))
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

func defineFormats() {
	openapi3.DefineStringFormatValidator("uuid", openapi3.NewCallbackValidator(func(v string) error {
		if _, err := uuid.Parse(v); err != nil {
			return errors.New("must be a valid UUID")
		}
		return nil
	}))
	openapi3.DefineStringFormatValidator("email", openapi3.NewCallbackValidator(func(v string) error {
		if addr, err := mail.ParseAddress(v); err != nil || addr.Address != v {
			return errors.New("must be a valid email address")
		}
		return nil
	}))
	openapi3.DefineNumberFormatValidator("decimal", openapi3.NewCallbackValidator(func(v float64) error {
		if cents := v * 100; math.Abs(cents-math.Round(cents)) > 1e-6 {
			return errors.New("must have at most 2 decimal places")
		}
		return nil
	}))
}

type violation struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

func collectViolations(err error) []violation {
	var out []violation
	var walk func(err error, field string)
	walk = func(err error, field string) {
		switch e := err.(type) {
		case openapi3.MultiError:
			for _, nested := range e {
				walk(nested, field)
			}
		case *openapi3filter.RequestError:
			if e.Parameter != nil {
				field = e.Parameter.Name
			}
			if e.Err != nil {
				walk(e.Err, field)
				return
			}
			out = append(out, violation{Field: orBody(field), Reason: e.Reason})
		case *openapi3.SchemaError:
			path := strings.Join(e.JSONPointer(), ".")
			if path == "" {
				path = field
			} else if field != "" {
				path = field + "." + path
			}
			out = append(out, violation{Field: orBody(path), Reason: e.Reason})
		case *openapi3filter.ParseError:
			out = append(out, violation{Field: orBody(field), Reason: e.Error()})
		default:
			if inner := errors.Unwrap(err); inner != nil {
				walk(inner, field)
				return
			}
			out = append(out, violation{Field: orBody(field), Reason: err.Error()})
		}
	}
	walk(err, "")
	if len(out) == 0 {
		out = append(out, violation{Field: "body", Reason: fmt.Sprint(err)})
	}
	return out
}

func orBody(field string) string {
	if field == "" {
		return "body"
	}
	return field
}
