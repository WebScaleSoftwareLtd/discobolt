package discobolt

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/vmihailenco/msgpack"
	"gopkg.in/yaml.v3"
)

type contextBase struct {
	context.Context

	req *http.Request
	w   http.ResponseWriter
	r   *Router

	consumed bool
}

// Context is used to define the HTTP context.
type Context struct {
	*contextBase

	pathRemainder []byte
	handlers      []handler
}

func (c *Context) addHandler(h handler) {
	if c.consumed {
		return
	}
	c.handlers = append(c.handlers, h)
	sort.Sort(routesSorter{a: c.handlers})
}

// Handles any errors that occur.
func (c *Context) handleError(err error) {
	// Try and hunt the user facing error.
	var userErr UserFacingError
	nextErr := err
	for nextErr != nil {
		if ue, ok := nextErr.(UserFacingError); ok {
			userErr = ue
			break
		}
		nextErr = errors.Unwrap(nextErr)
	}

	// If we have a user facing error, use it.
	if userErr != nil {
		err = c.consumeHandler(userErr.Status(), userErr.Body())
		if err == nil {
			// The error was successfully pushed out to the user.
			return
		}
	}

	// If we have an error handler, use it.
	if c.r.errHandler != nil {
		result, status := c.r.errHandler(c, err)
		err = c.consumeHandler(status, result)
		if err == nil {
			// The error was successfully pushed out to the user.
			return
		}
	}

	// Make the best of a shit situation.
	message := "Internal Server Error"
	status := 500
	if errors.Is(err, RouteNotFound) {
		message = "Not Found"
		status = 404
	}
	_ = c.consumeHandler(status, map[string]string{"message": message})
}

type wrapsString struct {
	s string
}

func (w wrapsString) String() string { return w.s }

// Used to consume the context. The main output handler for the web framework.
func (c *Context) consumeHandler(status int, body any) (err error) {
	if c.consumed {
		// We've already consumed the context.
		return nil
	}

	// If the status is 204, we don't need to send anything.
	if status == 204 {
		c.w.WriteHeader(status)
		c.consumed = true
		return nil
	}

	// Handle getting the Accept header.
	accept := c.req.Header.Get("Accept")
	if accept == "" {
		// Default to JSON.
		accept = "application/json"
	}

	// Handles setting the consumed state.
	defer func() {
		if err == nil {
			c.consumed = true
		}
	}()

	// Generally the default, so up here as its own thing.
	jsonSend := func() error {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		c.w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		c.w.Header().Set("Content-Type", "application/json")
		c.w.WriteHeader(status)
		_, _ = c.w.Write(b)
		return nil
	}

	// Split the accept header by comma and go through each part.
	acceptParts := strings.Split(accept, ",")
	for _, acceptPart := range acceptParts {
		// Trim the whitespace.
		acceptPart = strings.TrimSpace(acceptPart)

		// Split by semi-colon.
		acceptPartParts := strings.SplitN(acceptPart, ";", 1)
		contentType := acceptPartParts[0]
		switch contentType {
		case "application/json", "application/*", "*/*":
			err = jsonSend()
			return
		case "application/xml", "text/xml":
			b, err := xml.Marshal(body)
			if err != nil {
				return err
			}
			c.w.Header().Set("Content-Length", strconv.Itoa(len(b)))
			c.w.Header().Set("Content-Type", contentType)
			c.w.WriteHeader(status)
			_, _ = c.w.Write(b)
			return nil
		case "application/x-msgpack", "application/msgpack":
			var buf bytes.Buffer
			if err = msgpack.NewEncoder(&buf).UseJSONTag(true).Encode(body); err != nil {
				return
			}
			c.w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
			c.w.Header().Set("Content-Type", contentType)
			c.w.WriteHeader(status)
			_, _ = c.w.Write(buf.Bytes())
			return nil
		case "text/plain", "text/*":
			type stringer interface {
				String() string
			}
			if s, ok := body.(string); ok {
				body = wrapsString{s}
			}
			if st, ok := body.(stringer); ok {
				s := st.String()
				c.w.Header().Set("Content-Length", strconv.Itoa(len(s)))
				c.w.Header().Set("Content-Type", "text/plain")
				c.w.WriteHeader(status)
				_, _ = c.w.Write([]byte(s))
				return nil
			}
		case "text/html", "application/html":
			type htmler interface {
				HTML() ([]byte, error)
			}
			var b []byte
			if ht, ok := body.(htmler); ok {
				b, err = ht.HTML()
				if err != nil {
					return
				}
				c.w.Header().Set("Content-Length", strconv.Itoa(len(b)))
				c.w.Header().Set("Content-Type", contentType)
				c.w.WriteHeader(status)
				_, _ = c.w.Write(b)
				return nil
			}
		case "application/yaml", "text/yaml":
			b, err := yaml.Marshal(body)
			if err != nil {
				return err
			}
			c.w.Header().Set("Content-Length", strconv.Itoa(len(b)))
			c.w.Header().Set("Content-Type", contentType)
			c.w.WriteHeader(status)
			_, _ = c.w.Write(b)
			return nil
		}
	}

	// If we get here, we didn't find a matching Accept header. Just give them application/json.
	err = jsonSend()
	return
}

// Executed after a group is done with its function.
func (c *Context) afterExecute() {
	if c.consumed {
		return
	}
	for _, h := range c.handlers {
		ok, remainder, val := h.check(c.pathRemainder)
		if ok {
			// This is the route! Proceed with this.
			ctx := &Context{
				contextBase:   c.contextBase,
				pathRemainder: remainder,
			}
			h.execute(ctx, val)
			if ctx.consumed {
				// This route consumed it all.
				return
			}
		}
	}
}

func methodHandler[T any](c *Context, method string, handler func() (T, error), inputs []any) {
	// Handle preliminary checks.
	if c.consumed || c.req.Method != method {
		return
	}

	// Handle checking if the remaining path supports this.
	remainderLen := len(c.pathRemainder)
	if remainderLen > 0 {
		// Check if the length is 1 and the first character is a slash.
		if remainderLen == 1 && c.pathRemainder[0] == '/' {
			// TODO: figure out how we want to handle trailing slashes.
			return
		}

		// This is not ours to consume.
		return
	}

	// Get the content type and if applicable the body.
	contentType := c.req.Header.Get("Content-Type")
	var postedBody []byte
	if method == "GET" {
		// It doesn't actually matter what the content type is, the type should become application/x-www-form-urlencoded.
		contentType = "application/x-www-form-urlencoded"
	} else {
		if contentType != "application/x-www-form-urlencoded" {
			// Read the body up to the limit set on the router.
			limit := c.r.maxBodySize
			if limit == 0 {
				// Default to 2MB.
				limit = 2 * 1024 * 1024
			}
			postedBody, _ = io.ReadAll(io.LimitReader(c.req.Body, int64(limit)))
		}
	}

	// Go through each input and parse it.
	for _, v := range inputs {
		switch contentType {
		case "application/json":
			if err := json.Unmarshal(postedBody, v); err != nil {
				c.handleError(err)
				return
			}
		case "application/xml", "text/xml":
			if err := xml.Unmarshal(postedBody, v); err != nil {
				c.handleError(err)
				return
			}
		case "application/x-msgpack", "application/msgpack":
			if err := msgpack.NewDecoder(bytes.NewReader(postedBody)).UseJSONTag(true).Decode(v); err != nil {
				c.handleError(err)
				return
			}
		case "application/yaml", "text/yaml":
			if err := yaml.Unmarshal(postedBody, v); err != nil {
				c.handleError(err)
				return
			}
		case "application/x-www-form-urlencoded":
			// TODO
		default:
			// TODO
		}
	}

	// Call the handler.
	result, err := handler()
	if err != nil {
		c.handleError(err)
		return
	}

	// Set the status depending on what this is.
	status := 200
	reflectVal := reflect.ValueOf(result)
	switch reflectVal.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Func, reflect.Slice, reflect.Chan:
		if reflectVal.IsNil() {
			status = 204
		}
	}

	// Handle sending the result to the client.
	err = c.consumeHandler(status, result)
	if err != nil {
		c.handleError(err)
		return
	}
}
