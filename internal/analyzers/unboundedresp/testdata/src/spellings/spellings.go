// Package spellings holds the unbounded response reads the 2026-11-02
// guard bash found unboundedresp blind to: the body copied rather than
// ReadAll'd (io.Copy, bytes.Buffer.ReadFrom), the deprecated
// ioutil.ReadAll, and the body read through a non-limiting wrapper
// (bufio.NewReader) or a local alias of it.
package spellings

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
)

func copyAll(c *http.Client, url string) ([]byte, error) {
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, err = io.Copy(&buf, resp.Body) // want `unbounded read of an \*http.Response body`
	return buf.Bytes(), err
}

func readFrom(c *http.Client, url string) ([]byte, error) {
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, err = buf.ReadFrom(resp.Body) // want `unbounded read of an \*http.Response body`
	return buf.Bytes(), err
}

func deprecated(c *http.Client, url string) ([]byte, error) {
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body) // want `unbounded read of an \*http.Response body`
}

func wrapped(c *http.Client, url string, v any) error {
	resp, err := c.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(bufio.NewReader(resp.Body)).Decode(v) // want `unbounded read of an \*http.Response body`
}

func aliased(c *http.Client, url string) ([]byte, error) {
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body := resp.Body
	return io.ReadAll(body) // want `unbounded read of an \*http.Response body`
}

// Silent: the copy reads through the limiter, and a drain into
// io.Discard (the keep-alive idiom) holds no memory.
func drain(c *http.Client, url string) error {
	resp, err := c.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.Copy(io.Discard, resp.Body)
	return err
}

func copyCapped(c *http.Client, url string) ([]byte, error) {
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, err = io.Copy(&buf, io.LimitReader(resp.Body, 1<<20))
	return buf.Bytes(), err
}
