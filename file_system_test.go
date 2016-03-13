package zipfs

import (
	"archive/zip"
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			f.Close()

			// testing error after closing
			var buf [50]byte
			_, err := f.Read(buf[:])
			assert.Error(err)
			_, err = f.Seek(20, 0)
			assert.Error(err)
		} else {
			assert.Error(err)
			assert.True(strings.Contains(err.Error(), tc.Error), err.Error())
			assert.True(strings.Contains(err.Error(), tc.Path), err.Error())
		}
	}

	err = fs.Close()
	assert.NoError(err)
	f, err := fs.Open("/img/circle.png")
	assert.Error(err)
	assert.Nil(f)
	assert.True(strings.Contains(err.Error(), "filesystem closed"), err.Error())
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

// TestFileInfo tests the os.FileInfo associated with the http.File
func TestFileInfo(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)
	fs, err := New("testdata/testdata.zip")
	require.NoError(err)

	testCases := []struct {
		Path       string
		Name       string
		Size       int64
		Mode       os.FileMode
		IsDir      bool
		HasZipFile bool
	}{
		// Don't use any text files here because the sizes
		// are different betwen Windows and Unix-like OSs.
		{
			Path:       "/img/circle.png",
			Name:       "circle.png",
			Size:       5973,
			Mode:       0444,
			IsDir:      false,
			HasZipFile: true,
		},
		{
			Path:       "/img/",
			Name:       "img",
			Size:       0,
			Mode:       os.ModeDir | 0555,
			IsDir:      true,
			HasZipFile: true,
		},
		{
			Path:       "/",
			Name:       "/",
			Size:       0,
			Mode:       os.ModeDir | 0555,
			IsDir:      true,
			HasZipFile: true,
		},
	}

	for _, tc := range testCases {
		file, err := fs.Open(tc.Path)
		require.NoError(err)
		fi, err := file.Stat()
		require.NoError(err)
		assert.Equal(tc.Name, fi.Name())
		assert.Equal(tc.Size, fi.Size())
		assert.Equal(tc.Mode, fi.Mode())
		assert.Equal(tc.IsDir, fi.IsDir())
		_, hasZipFile := fi.Sys().(*zip.File)
		assert.Equal(tc.HasZipFile, hasZipFile, fi.Name())
		assert.False(fi.ModTime().IsZero())
	}
}

// TestFile tests the file reading capabilities.
func TestFile(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)
	fs, err := New("testdata/testdata.zip")
	require.NoError(err)

	testCases := []struct {
		Path string
		Size int
		MD5  string
	}{
		{
			Path: "/random.dat",
			Size: 10000,
			MD5:  "3c9fe0521cabb2ab38484cd1c024a61d",
		},
		{
			Path: "/img/circle.png",
			Size: 5973,
			MD5:  "05e3048db45e71749e06658ccfc0753b",
		},
	}

	calcMD5 := func(r io.ReadSeeker, size int, seek bool) string {
		if seek {
			n, err := r.Seek(0, 0)
			require.NoError(err)
			require.Equal(int64(0), n)
		}
		buf := make([]byte, size)
		n, err := r.Read(buf)
		require.NoError(err)
		require.Equal(size, n)
		md5Text := fmt.Sprintf("%x", md5.Sum(buf))
		n, err = r.Read(buf)
		require.Error(err)
		require.Equal(io.EOF, err)
		require.Equal(0, n)
		return md5Text
	}

	for _, tc := range testCases {
		file, err := fs.Open(tc.Path)
		assert.NoError(err)
		assert.Equal(tc.MD5, calcMD5(file, tc.Size, false))

		// seek back to the beginning, should not have
		// to create a temporary file
		nseek, err := file.Seek(0, 0)
		assert.NoError(err)
		assert.Equal(int64(0), nseek)
		assert.Equal(tc.MD5, calcMD5(file, tc.Size, true))

		nSeek, err := file.Seek(int64(tc.Size/2), 0)
		assert.NoError(err)
		assert.Equal(int64(tc.Size/2), nSeek)
		assert.Equal(tc.MD5, calcMD5(file, tc.Size, true))

		file.Close()
	}
}
