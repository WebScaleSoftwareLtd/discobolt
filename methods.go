package discobolt

// TODO: add all http routes

// GET is used to define a GET request in the current route context.
func GET[T any](c *Context, handler func() (T, error), inputs ...any) {
	methodHandler(c, "GET", handler, inputs)
}

// POST is used to define a POST request in the current route context.
func POST[T any](c *Context, handler func() (T, error), inputs ...any) {
	methodHandler(c, "POST", handler, inputs)
}
