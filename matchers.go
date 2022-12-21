package discobolt

import (
	"net/url"
	"strconv"
)

// RouterOrContext is used to define a interface that can be used for either *Router or *Context.
type RouterOrContext interface {
	addHandler(h handler)
}

// Consume the part of the path until the next slash. Returns a slice with the contents and the remainder of the path.
func consumeUntilSlash(path []byte) (contents, remainder []byte) {
sliceRunner:
	for i, b := range path {
		if b == '/' {
			if i == 0 {
				path = path[1:]
				goto sliceRunner
			}
			return path[:i], path[i:]
		}
	}
	return path, []byte{}
}

// Static is used to match based on the text content specified.
func Static(c RouterOrContext, text string, hn func(*Context)) {
	h := handler{
		check: func(path []byte) (bool, []byte, any) {
			contents, remainder := consumeUntilSlash(path)
			if string(contents) == text {
				return true, remainder, nil
			}
			return false, path, nil
		},
		execute: func(ctx *Context, _ any) {
			hn(ctx)
			ctx.afterExecute()
		},
		priority: 2,
	}
	c.addHandler(h)
}

// Int is used to match a signed integer.
func Int(c RouterOrContext, hn func(*Context, int)) {
	h := handler{
		check: func(path []byte) (bool, []byte, any) {
			contents, remainder := consumeUntilSlash(path)
			i, err := strconv.Atoi(string(contents))
			if err != nil {
				return false, path, nil
			}
			return true, remainder, i
		},
		execute: func(ctx *Context, i any) {
			hn(ctx, i.(int))
			ctx.afterExecute()
		},
		priority: 1,
	}
	c.addHandler(h)
}

// Uint is used to match an unsigned integer.
func Uint(c RouterOrContext, hn func(*Context, uint64)) {
	h := handler{
		check: func(path []byte) (bool, []byte, any) {
			contents, remainder := consumeUntilSlash(path)
			i, err := strconv.ParseUint(string(contents), 10, 64)
			if err != nil {
				return false, path, nil
			}
			return true, remainder, i
		},
		execute: func(ctx *Context, i any) {
			hn(ctx, i.(uint64))
			ctx.afterExecute()
		},
		priority: 1,
	}
	c.addHandler(h)
}

// Float is used to match a floating point number.
func Float(c RouterOrContext, hn func(*Context, float64)) {
	h := handler{
		check: func(path []byte) (bool, []byte, any) {
			contents, remainder := consumeUntilSlash(path)
			i, err := strconv.ParseFloat(string(contents), 64)
			if err != nil {
				return false, path, nil
			}
			return true, remainder, i
		},
		execute: func(ctx *Context, i any) {
			hn(ctx, i.(float64))
			ctx.afterExecute()
		},
		priority: 1,
	}
	c.addHandler(h)
}

// String is used to match a string.
func String(c RouterOrContext, hn func(*Context, string)) {
	h := handler{
		check: func(path []byte) (bool, []byte, any) {
			contents, remainder := consumeUntilSlash(path)
			if len(contents) == 0 {
				return false, path, nil
			}
			x, err := url.PathUnescape(string(contents))
			if err != nil {
				return false, path, nil
			}
			return true, remainder, x
		},
		execute: func(ctx *Context, i any) {
			hn(ctx, i.(string))
			ctx.afterExecute()
		},
		priority: 1,
	}
	c.addHandler(h)
}

// Remainder is used to match the remainder of the path when there is more than 1 char after it. Returns the raw result.
func Remainder(c RouterOrContext, hn func(*Context, string)) {
	h := handler{
		check: func(path []byte) (bool, []byte, any) {
			if len(path) > 1 {
				return true, []byte{}, path
			}
			return false, path, nil
		},
		execute: func(ctx *Context, i any) {
			hn(ctx, string(i.([]byte)))
			ctx.afterExecute()
		},
		priority: 2,
	}
	c.addHandler(h)
}
