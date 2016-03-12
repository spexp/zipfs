package zipfs

import (
	"bytes"
	"fmt"
	"io"
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
			Path:   "/img/circle.png",
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
			Path:   "/img/circle.png",
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
			Path:   "/index.html",
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
			ContentLength:   "134",
			ContentEncoding: "",
			Size:            134,
			ETag:            `"5900c7c98f"`,
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

func TestOpen(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)
	fs, err := New("testdata/testdata.zip")
	require.NoError(err)

	testCases := []struct {
		Path  string
		Error string
	}{
		{
			Path:  "/does/not/exist",
			Error: "file does not exist",
		},
		{
			Path:  "/img",
			Error: "",
		},
		{
			Path:  "/img/circle.png",
			Error: "",
		},
	}
	for _, tc := range testCases {
		f, err := fs.Open(tc.Path)
		if tc.Error == "" {
			assert.NoError(err)
			assert.NotNil(f)
		} else {
			assert.Error(err)
			assert.True(strings.Contains(err.Error(), tc.Error), err.Error())
			assert.True(strings.Contains(err.Error(), tc.Path), err.Error())
		}
	}
}

func TestReaddir(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)
	fs, err := New("testdata/testdata.zip")
	require.NoError(err)

	testCases := []struct {
		Path  string
		Count int
		Error string
		Files []string
	}{
		{
			Path:  "/img",
			Error: "",
			Files: []string{
				"another-circle.png",
				"circle.png",
			},
		},
		{
			Path:  "/",
			Error: "",
			Files: []string{
				"empty",
				"img",
				"index.html",
				"js",
				"lots-of-files",
				"not-a-zip-file.txt",
				"random.dat",
				"test.html",
			},
		},
		{
			Path:  "/lots-of-files",
			Error: "",
			Files: []string{
				"file-01",
				"file-02",
				"file-03",
				"file-04",
				"file-05",
				"file-06",
				"file-07",
				"file-08",
				"file-09",
				"file-10",
				"file-11",
				"file-12",
				"file-13",
				"file-14",
				"file-15",
				"file-16",
				"file-17",
				"file-18",
				"file-19",
				"file-20",
			},
		},
		{
			Path:  "/img/circle.png",
			Error: "not a directory",
		},
		{
			Path:  "/img/circle.png",
			Error: "not a directory",
			Count: 2,
		},
	}

	for _, tc := range testCases {
		f, err := fs.Open(tc.Path)
		require.NoError(err)
		require.NotNil(f)

		files, err := f.Readdir(tc.Count)
		if tc.Error == "" {
			assert.NoError(err)
			assert.NotNil(files)
			printError := false
			if len(files) != len(tc.Files) {
				printError = true
			} else {
				for i, file := range files {
					if file.Name() != tc.Files[i] {
						printError = true
						break
					}
				}
			}
			if printError {
				t.Log(tc.Path, "Readdir expected:")
				for i, f := range tc.Files {
					t.Logf("    %d: %s\n", i, f)
				}
				t.Log(tc.Path, "Readdir actual:")
				for i, f := range files {
					t.Logf("    %d: %s\n", i, f.Name())
				}
				t.Error("Readdir failed test")
			}
		} else {
			assert.Error(err)
			assert.Nil(files)
			assert.True(strings.Contains(err.Error(), tc.Error), err.Error())
			assert.True(strings.Contains(err.Error(), tc.Path), err.Error())
		}
	}

	file, err := fs.Open("/lots-of-files")
	require.NoError(err)
	for i := 0; i < 10; i++ {
		a, err := file.Readdir(2)
		require.NoError(err)
		assert.Equal(len(a), 2)
		assert.Equal(fmt.Sprintf("file-%02d", i*2+1), a[0].Name())
		assert.Equal(fmt.Sprintf("file-%02d", i*2+2), a[1].Name())
	}
	a, err := file.Readdir(2)
	assert.Error(err)
	assert.Equal(io.EOF, err)
	assert.Equal(0, len(a))
}
