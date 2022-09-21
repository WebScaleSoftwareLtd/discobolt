package discobolt

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/gorilla/schema"
	"github.com/gorilla/websocket"
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

// Check is used to check if the current route passes a check. If error is not nil, execution will be aborted and
// the error will be returned to the user.
type Check func() error

// Context is used to define the HTTP context.
type Context struct {
	*contextBase

	// Defines the values needed for websocket handling. It kinda sucks that we need to save GET until the end,
	// but it does mean that we can manage this better.
	webSocketUpgrader *websocket.Upgrader
	webSocketHandler  func(*websocket.Conn) error
	getRunner         func()

	pathRemainder []byte
	handlers      []handler
	checks        []Check
}

// RequestHeaders returns the request headers.
func (c *Context) RequestHeaders() http.Header {
	return c.req.Header
}

// ResponseHeaders returns the response headers.
func (c *Context) ResponseHeaders() http.Header {
	return c.w.Header()
}

// URL returns the URL of the request.
func (c *Context) URL() *url.URL {
	return c.req.URL
}

// RemoteIP returns the remote IP address. If the request is behind a known proxy IP, it will try to get the real IP.
// Supported proxies are currently Cloudflare and Fastly.
func (c *Context) RemoteIP() net.IP {
	ipS, _, err := net.SplitHostPort(c.req.RemoteAddr)
	if err != nil {
		return nil
	}
	ip := net.ParseIP(ipS)
	if !c.r.disableAutoProxy {
		header := evalIp(ip)
		if header != "" {
			h := c.req.Header.Get(header)
			if h != "" {
				return net.ParseIP(h)
			}
		}
	}
	return ip
}

// AddCheck adds a check to the context.
func AddCheck(ctx *Context, check Check) {
	ctx.checks = append(ctx.checks, check)
}

func (c *Context) addHandler(h handler) {
	if c.consumed {
		return
	}
	c.handlers = append(c.handlers, h)
	sort.Sort(routesSorter{a: c.handlers})
}

// IsBadRequest returns true if the error is a bad request error.
func IsBadRequest(err error) bool {
	var badReqErr *BadRequest
	nextErr := err
	for nextErr != nil {
		if br, ok := nextErr.(BadRequest); ok {
			badReqErr = &br
			break
		}
		nextErr = errors.Unwrap(nextErr)
	}
	return badReqErr != nil
}

// Redirect is a special type that when detected will lead to a redirect.
type Redirect struct {
	URL       string
	Permanent bool
}

// Error implements the error interface. This allows you to throw a redirect as a error and have it magically handled.
func (Redirect) Error() string { return "redirect" }

// Body returns itself to implement UserFacingError. This allows you to throw a redirect as a error and have it magically handled.
func (r Redirect) Body() any { return r }

// Status returns nothing and is just here to implement UserFacingError. This allows you to throw a redirect as a error and have it magically handled.
func (Redirect) Status() int { return 0 }

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
		// Is just a not found error.
		message = "Not Found"
		status = 404
	} else if IsBadRequest(err) {
		// Is a bad request error.
		message = "Bad Request"
		status = 400
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

	// Handle if this is a redirect.
	if re, ok := body.(*Redirect); ok {
		if re == nil {
			return errors.New("nil pointer to special redirect struct")
		}
		body = *re
	}
	if re, ok := body.(Redirect); ok {
		code := http.StatusTemporaryRedirect
		if re.Permanent {
			code = http.StatusPermanentRedirect
		}
		http.Redirect(c.w, c.req, re.URL, code)
		c.consumed = true
		return nil
	}

	// Handle getting the Accept header.
	accept := c.req.Header.Get("Accept")
	if accept == "" {
		// Try setting it to the content type.
		accept = c.req.Header.Get("Content-Type")
		if accept == "" {
			// Default to JSON.
			accept = "application/json"
		}
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

// Runs all checks.
func (c *Context) runChecks() (err error) {
	for _, check := range c.checks {
		if err = check(); err != nil {
			c.handleError(err)
			return
		}
	}
	return
}

// Executed after a group is done with its function.
func (c *Context) afterExecute() {
	if c.consumed {
		return
	}

	// Add panic protection.
	defer func() {
		if errPossibly := recover(); errPossibly != nil {
			var err error
			if errPossibly, ok := errPossibly.(error); ok {
				err = errPossibly
			} else {
				err = fmt.Errorf("%v", errPossibly)
			}
			c.handleError(err)
		}
	}()

	if c.req.Method == "GET" {
		if c.webSocketUpgrader == nil {
			// Just run the GET handler.
			if c.getRunner != nil {
				c.getRunner()
			}
		} else if len(c.pathRemainder) == 0 {
			if strings.Contains(strings.ToLower(c.req.Header.Get("Connection")), "upgrade") &&
				strings.ToLower(c.req.Header.Get("Upgrade")) == "websocket" {
				// Upgrade to a websocket.
				conn, err := c.webSocketUpgrader.Upgrade(c.w, c.req, nil)
				c.consumed = true
				if err != nil {
					// Return here. This error is a bit special.
					return
				}
				if err = c.webSocketHandler(conn); err != nil {
					// Ok fine. The least worse thing here is to not output to the user the error info.
					c.handleError(err)
				}
				return
			}

			// Run the GET handler.
			if c.getRunner != nil {
				c.getRunner()
			}
		}
	}

	if err := c.runChecks(); err != nil {
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

var (
	queryDecoder = schema.NewDecoder()
	formDecoder  = schema.NewDecoder()
)

func init() {
	queryDecoder.SetAliasTag("query")
	formDecoder.SetAliasTag("form")
}

func methodHandler[T any](c *Context, method string, handler func() (T, error), inputs []any) {
	// Handle preliminary checks.
	if c.consumed || c.req.Method != method {
		return
	}

	// Run all the checks within this context.
	if err := c.runChecks(); err != nil {
		return
	}

	// Handle checking if the remaining path supports this.
	if len(c.pathRemainder) > 0 {
		// This is not ours to consume.
		return
	}

	// Get the memory limit.
	limit := c.r.maxBodySize
	if limit == 0 {
		// Default to 2MB.
		limit = 2 * 1024 * 1024
	}

	// Get the content type and if applicable the body.
	contentType := c.req.Header.Get("Content-Type")
	var postedBody []byte
	if method == "GET" {
		// It doesn't actually matter what the content type is, the type should become application/x-www-form-urlencoded.
		contentType = "application/x-www-form-urlencoded"
	} else {
		// Read the body up to the limit set on the router.
		postedBody, _ = io.ReadAll(io.LimitReader(c.req.Body, int64(limit)))
	}

	// Go through each input and parse it.
	for _, v := range inputs {
		switch contentType {
		case "application/json":
			if err := json.Unmarshal(postedBody, v); err != nil {
				c.handleError(BadRequest{err})
				return
			}
		case "application/xml", "text/xml":
			if err := xml.Unmarshal(postedBody, v); err != nil {
				c.handleError(BadRequest{err})
				return
			}
		case "application/x-msgpack", "application/msgpack":
			if err := msgpack.NewDecoder(bytes.NewReader(postedBody)).UseJSONTag(true).Decode(v); err != nil {
				c.handleError(BadRequest{err})
				return
			}
		case "application/yaml", "text/yaml":
			if err := yaml.Unmarshal(postedBody, v); err != nil {
				c.handleError(BadRequest{err})
				return
			}
		case "application/x-www-form-urlencoded":
			var query url.Values
			if len(postedBody) > 0 {
				query, _ = url.ParseQuery(string(postedBody))
			} else {
				query = c.req.URL.Query()
			}
			if err := queryDecoder.Decode(v, query); err != nil {
				c.handleError(BadRequest{err})
				return
			}
		default:
			// Handle multipart form data.
			if strings.HasPrefix(contentType, "multipart/form-data") {
				if err := c.req.ParseMultipartForm(int64(limit)); err != nil {
					c.handleError(BadRequest{err})
					return
				}
				if err := formDecoder.Decode(v, c.req.MultipartForm.Value); err != nil {
					c.handleError(BadRequest{err})
					return
				}
			} else {
				// Check if this is io.Writer.
				if w, ok := v.(io.Writer); ok {
					// Write the body to the writer.
					_, _ = w.Write(postedBody)
				} else {
					// Assume JSON if there is no content type.
					if err := json.Unmarshal(postedBody, v); err != nil {
						c.handleError(BadRequest{err})
						return
					}
				}
			}
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
