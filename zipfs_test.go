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

func TestFileSystem(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	fs, err := New("testdata/testdata.zip")
	require.NoError(err)
	require.NotNil(fs)

	f, err := fs.Open("/xxx")
	assert.Error(err)
	assert.Nil(f)

	f, err = fs.Open("test.html")
	assert.NoError(err)
	assert.NotNil(f)

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
		ETag            string
		Size            int
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
			Size:            4758,
			ETag:            `"1296529fb2ff"`,
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
			Size:            5973,
			ETag:            `"1296529fb2ff"`,
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
			ETag:            `"5532e54275"`,
		},
		{
			Path:            "/test.html",
			Status:          200,
			Headers:         []string{},
			ContentType:     "text/html; charset=utf-8",
			ContentLength:   "122",
			ContentEncoding: "",
			Size:            122,
			ETag:            `"5532e54275"`,
		},
		{
			Path:   "/does/not/exist",
			Status: 404,
			Headers: []string{
				"Accept-Encoding: deflate, gzip",
			},
			ContentType: "text/plain; charset=utf-8",
		},
		{
			Path:   "/random.dat",
			Status: 200,
			Headers: []string{
				"Accept-Encoding: deflate",
			},
			ContentType:     "application/octet-stream",
			ContentLength:   "10000",
			ContentEncoding: "",
			Size:            10000,
			ETag:            `"27106c15f45b"`,
		},
		{
			Path:            "/random.dat",
			Status:          200,
			Headers:         []string{},
			ContentType:     "application/octet-stream",
			ContentLength:   "10000",
			ContentEncoding: "",
			Size:            10000,
			ETag:            `"27106c15f45b"`,
		},
		{
			Path:   "/random.dat",
			Status: 206,
			Headers: []string{
				`If-Range: "27106c15f45b"`,
				"Range: bytes=0-499",
			},
			ContentType:     "application/octet-stream",
			ContentLength:   "500",
			ContentEncoding: "",
			Size:            500,
			ETag:            `"27106c15f45b"`,
		},
		{
			Path:   "/random.dat",
			Status: 200,
			Headers: []string{
				`If-Range: "123456789"`,
				"Range: bytes=0-499",
				"Accept-Encoding: deflate, gzip",
			},
			ContentType:     "application/octet-stream",
			ContentLength:   "10000",
			ContentEncoding: "",
			Size:            10000,
			ETag:            `"27106c15f45b"`,
		},
		{
			Path:   "/random.dat",
			Status: 304,
			Headers: []string{
				`If-None-Match: "27106c15f45b"`,
				"Accept-Encoding: deflate, gzip",
			},
			ContentType:     "",
			ContentLength:   "",
			ContentEncoding: "",
			Size:            0,
			ETag:            `"27106c15f45b"`,
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
			Method: "GET",
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
		if tc.Size > 0 {
			assert.Equal(tc.Size, w.buf.Len(), tc.Path)
		}
		if tc.ETag != "" {
			assert.Equal(tc.ETag, w.Header().Get("Etag"), tc.Path)
		}
	}

}
