package zipfs

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type TestResponseWriter struct {
	header http.Header
	status int
	buf    bytes.Buffer
}

func NewTestResponseWriter() *TestResponseWriter {
	return &TestResponseWriter{
		header: make(http.Header),
		status: 200,
	}
}

func (w *TestResponseWriter) Header() http.Header {
	return w.header
}

func (w *TestResponseWriter) Write(b []byte) (int, error) {
	return w.buf.Write(b)
}

func (w *TestResponseWriter) WriteHeader(status int) {
	w.status = status
}

func TestNew(t *testing.T) {
	assert := assert.New(t)
	testCases := []struct {
		Name  string
		Error string
	}{
		{
			Name:  "testdata/does-not-exist.zip",
			Error: "The system cannot find the file specified",
		},
		{
			Name:  "testdata/testdata.zip",
			Error: "",
		},
		{
			Name:  "testdata/not-a-zip-file.txt",
			Error: "zip: not a valid zip file",
		},
	}

	for _, tc := range testCases {
		fs, err := New(tc.Name)
		if tc.Error != "" {
			assert.Error(err)
			assert.True(strings.Contains(err.Error(), tc.Error), err.Error())
			assert.Nil(fs)
		} else {
			assert.NoError(err)
			assert.NotNil(fs)
		}
		if fs != nil {
			fs.Close()
		}
	}
}

func TestServeHTTP(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	fs, err := New("testdata/testdata.zip")
	require.NoError(err)
	require.NotNil(fs)

	handler := FileServer(fs)

	testCases := []struct {
		Path            string
		Headers         []string
		Status          int
		ContentType     string
		ContentLength   string
		ContentEncoding string
	}{
		{
			Path:   "/circle.png",
			Status: 200,
			Headers: []string{
				"Accept-Encoding: deflate, gzip",
			},
			ContentType:     "image/png",
			ContentLength:   "4758",
			ContentEncoding: "deflate",
		},
		{
			Path:   "/circle.png",
			Status: 200,
			Headers: []string{
				"Accept-Encoding: gzip",
			},
			ContentType:     "image/png",
			ContentLength:   "5973",
			ContentEncoding: "",
		},
		{
			Path:   "/test.html",
			Status: 200,
			Headers: []string{
				"Accept-Encoding: deflate, gzip",
			},
			ContentType:     "text/html; charset=utf-8",
			ContentLength:   "85",
			ContentEncoding: "deflate",
		},
		{
			Path:            "/test.html",
			Status:          200,
			Headers:         []string{},
			ContentType:     "text/html; charset=utf-8",
			ContentLength:   "122",
			ContentEncoding: "",
		},
		{
			Path:   "/does/not/exist",
			Status: 404,
			Headers: []string{
				"Accept-Encoding: deflate, gzip",
			},
			ContentType: "text/plain; charset=utf-8",
		},
	}

	for _, tc := range testCases {
		req := &http.Request{
			URL: &url.URL{
				Scheme: "http",
				Host:   "test-server.com",
				Path:   tc.Path,
			},
			Header: make(http.Header),
		}

		for _, header := range tc.Headers {
			arr := strings.SplitN(header, ":", 2)
			key := strings.TrimSpace(arr[0])
			value := strings.TrimSpace(arr[1])
			req.Header.Add(key, value)
		}

		w := NewTestResponseWriter()
		handler.ServeHTTP(w, req)

		assert.Equal(tc.Status, w.status)
		assert.Equal(tc.ContentType, w.Header().Get("Content-Type"), tc.Path)
		assert.Equal(tc.ContentLength, w.Header().Get("Content-Length"), tc.Path)
		assert.Equal(tc.ContentEncoding, w.Header().Get("Content-Encoding"), tc.Path)
	}

}
