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
	"os"
	"strings"
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

type byteSink []byte

func (b *byteSink) Write(p []byte) (int, error) { *b = append(*b, p...); return len(p), nil }

func copyBuilder(c *http.Client, url string) (string, error) {
	resp, err := c.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var sb strings.Builder
	_, err = io.CopyBuffer(&sb, resp.Body, nil) // want `unbounded read of an \*http.Response body`
	return sb.String(), err
}

func copySlice(c *http.Client, url string) ([]byte, error) {
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var sink byteSink
	_, err = io.Copy(&sink, resp.Body) // want `unbounded read of an \*http.Response body`
	return sink, err
}

// Silent: streaming into a writer that does not keep the bytes (a
// proxied ResponseWriter, a file) holds no memory, the same reason the
// io.Discard drain is quiet. Bounding a download's disk use is a
// different rule's business.
func proxy(c *http.Client, url string, w http.ResponseWriter) error {
	resp, err := c.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.Copy(w, resp.Body)
	return err
}

func download(c *http.Client, url string, f *os.File) error {
	resp, err := c.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	_, err = f.ReadFrom(resp.Body)
	return err
}

func streamTo(c *http.Client, url string, dst io.Writer) error {
	resp, err := c.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.Copy(dst, resp.Body)
	return err
}
