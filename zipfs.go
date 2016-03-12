package zipfs

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	errNotImplemented = errors.New("not implemented yet")
	errFileClosed     = errors.New("file closed")
	errNotDirectory   = errors.New("not a directory")
	errDirectory      = errors.New("is a directory")
)

// FileSystem is a file system based on a ZIP file.
// It currently does not, (but could) implement the
// http.FileSystem interface.
type FileSystem struct {
	readerAt  io.ReaderAt
	reader    *zip.Reader
	closer    io.Closer
	fileInfos fileInfoMap
}

// Open implements the http.FileSystem interface.
// A http.File is returned, which can be served by
// the http.FileServer implementation.
func (fs *FileSystem) Open(name string) (http.File, error) {
	name = path.Clean(name)
	trimmedName := strings.TrimLeft(name, "/")
	fi := fs.fileInfos[trimmedName]
	if fi == nil {
		return nil, &os.PathError{Op: "Open", Path: name, Err: os.ErrNotExist}
	}

	return fi.openReader(name), nil
}

// Close closes the file system's underlying ZIP file.
func (fs *FileSystem) Close() error {
	fs.reader = nil
	fs.readerAt = nil
	var err error
	if fs.closer != nil {
		err = fs.closer.Close()
		fs.closer = nil
	}
	return err
}

// New will open the Zip file specified by name and
// return a new FileSystem based on that Zip file.
func New(name string) (*FileSystem, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	fi, err := file.Stat()
	if err != nil {
		return nil, err
	}
	zipReader, err := zip.NewReader(file, fi.Size())
	if err != nil {
		return nil, err
	}
	fs := &FileSystem{
		closer:    file,
		readerAt:  file,
		reader:    zipReader,
		fileInfos: fileInfoMap{},
	}

	// Build a map of file paths to speed lookup.
	// Note that this assumes that there are not a very
	// large number of files in the ZIP file.
	//
	// Because we iterate through the map it seems reasonable
	// to attach each fileInfo to it's parent directory. Once again,
	// reasonable if the ZIP file does not contain a very large number
	// of entries.
	for _, zf := range fs.reader.File {
		fi := fs.fileInfos.FindOrCreate(zf.Name)
		fi.zipFile = zf
		dirEntry := fs.fileInfos.FindOrCreateParent(zf.Name)
		dirEntry.fileInfos = append(dirEntry.fileInfos, fi)
	}

	for _, fi := range fs.fileInfos {
		if len(fi.fileInfos) > 1 {
			sort.Sort(fi.fileInfos)
		}
	}

	return fs, nil
}

type fileInfoList []*fileInfo

func (fl fileInfoList) Len() int {
	return len(fl)
}

func (fl fileInfoList) Less(i, j int) bool {
	name1 := fl[i].Name()
	name2 := fl[j].Name()
	return strings.Compare(name1, name2) < 0
}

func (fl fileInfoList) Swap(i, j int) {
	fi := fl[i]
	fl[i] = fl[j]
	fl[j] = fi
}

// fileMap keeps track of fileInfos
type fileInfoMap map[string]*fileInfo

func (fm fileInfoMap) FindOrCreate(name string) *fileInfo {
	strippedName := strings.TrimRight(name, "/")
	fi := fm[name]
	if fi == nil {
		fi = &fileInfo{
			name: name,
		}
		fm[name] = fi
		if strippedName != name {
			// directories get two entries: with and without trailing slash
			fm[strippedName] = fi
		}
	}
	return fi
}

func (fm fileInfoMap) FindOrCreateParent(name string) *fileInfo {
	strippedName := strings.TrimRight(name, "/")
	dirName := path.Dir(strippedName)
	if dirName == "." {
		dirName = "/"
	} else if !strings.HasSuffix(dirName, "/") {
		dirName = dirName + "/"
	}
	return fm.FindOrCreate(dirName)
}

// fileInfo implements the os.FileInfo interface.
type fileInfo struct {
	name      string
	fs        *FileSystem
	zipFile   *zip.File
	fileInfos fileInfoList
	tempPath  string
	mutex     sync.Mutex
}

func (fi *fileInfo) Name() string {
	return path.Base(fi.name)
}

func (fi *fileInfo) Size() int64 {
	if fi.zipFile == nil {
		return 0
	}
	if fi.zipFile.UncompressedSize64 == 0 {
		return int64(fi.zipFile.UncompressedSize)
	}
	return int64(fi.zipFile.UncompressedSize64)
}

func (fi *fileInfo) Mode() os.FileMode {
	if fi.zipFile == nil || fi.IsDir() {
		return 0555 | os.ModeDir
	}
	return 0444
}

var dirTime = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

func (fi *fileInfo) ModTime() time.Time {
	if fi.zipFile == nil {
		return dirTime
	}
	return fi.zipFile.ModTime()
}

func (fi *fileInfo) IsDir() bool {
	if fi.zipFile == nil {
		return true
	}
	return fi.zipFile.Mode().IsDir()
}

func (fi *fileInfo) Sys() interface{} {
	return fi.zipFile
}

func (fi *fileInfo) openReader(name string) *fileReader {
	return &fileReader{
		fileInfo: fi,
		name:     name,
	}
}

func (fi *fileInfo) openFile() (*os.File, error) {
	fi.mutex.Lock()
	defer fi.mutex.Unlock()
	if fi.tempPath == "" {
		tempFile, err := createTempFile(fi.zipFile)
		if err != nil {
			return nil, err
		}

		// remember the name for next time
		fi.tempPath = tempFile.Name()
		return tempFile, nil
	}

	// temp file already exists
	return os.Open(fi.tempPath)
}

func (fi *fileInfo) readdir() ([]os.FileInfo, error) {
	if !fi.Mode().IsDir() {
		return nil, errNotDirectory
	}

	v := make([]os.FileInfo, len(fi.fileInfos))
	for i, fi := range fi.fileInfos {
		v[i] = fi
	}
	return v, nil
}

type fileReader struct {
	name     string // the name used to open
	fileInfo *fileInfo
	reader   io.ReadCloser
	file     *os.File
	closed   bool
	readdir  []os.FileInfo
}

func (f *fileReader) Close() error {
	var err1, err2 error
	if f.reader != nil {
		err1 = f.reader.Close()
	}
	if f.file != nil {
		err2 = f.file.Close()
	}

	f.closed = true
	if err1 != nil {
		return f.pathError("Close", err1)
	}
	if err2 != nil {
		return f.pathError("Close", err2)
	}
	return nil
}

func (f *fileReader) Read(p []byte) (n int, err error) {
	if f.closed {
		return 0, f.pathError("Read", errFileClosed)
	}
	if f.file != nil {
		return f.file.Read(p)
	}
	if f.reader == nil {
		f.reader, err = f.fileInfo.zipFile.Open()
		if err != nil {
			return 0, err
		}
	}
	return f.reader.Read(p)
}

func (f *fileReader) Seek(offset int64, whence int) (int64, error) {
	if f.closed {
		return 0, f.pathError("Seek", errFileClosed)
	}

	// The reader cannot seek, so close it.
	if f.reader != nil {
		if err := f.reader.Close(); err != nil {
			return 0, err
		}
	}
	if f.file == nil {
		// Open a file that contains the contents of the zip file.
		osFile, err := f.fileInfo.openFile()
		if err != nil {
			return 0, err
		}

		f.file = osFile
	}
	return f.file.Seek(offset, whence)
}

func (f *fileReader) Readdir(count int) ([]os.FileInfo, error) {
	var err error
	var osFileInfos []os.FileInfo

	if count > 0 {
		if f.readdir == nil {
			f.readdir, err = f.fileInfo.readdir()
			if err != nil {
				return nil, f.pathError("Readdir", err)
			}
		}
		if len(f.readdir) >= count {
			osFileInfos = f.readdir[0:count]
			f.readdir = f.readdir[count:]
		} else {
			osFileInfos = f.readdir
			f.readdir = nil
			err = io.EOF
		}
	} else {
		osFileInfos, err = f.fileInfo.readdir()
		if err != nil {
			return nil, f.pathError("Readdir", err)
		}
	}

	return osFileInfos, err
}

func (f *fileReader) Stat() (os.FileInfo, error) {
	return f.fileInfo, nil
}

func (f *fileReader) pathError(op string, err error) error {
	return &os.PathError{
		Op:   op,
		Path: f.name,
		Err:  err,
	}
}

// FileServer returns a HTTP handler that serves
// HTTP requests with the contents of the ZIP file system.
// It provides slightly better performance than the
// http.FileServer implementation because it serves compressed content
// to clients that can accept the "deflate" compression algorithm.
func FileServer(fs *FileSystem) http.Handler {
	return newHandler(fs)
}

type fileHandler struct {
	readerAt io.ReaderAt
	m        map[string]*zip.File
}

func newHandler(fs *FileSystem) *fileHandler {
	h := &fileHandler{
		readerAt: fs.readerAt,
		m:        make(map[string]*zip.File),
	}

	// Build a map of file paths to speed lookup.
	// Note that this assumes that there are not a very
	// large number of files in the ZIP file.
	for _, f := range fs.reader.File {
		// include the leading slash in the key because that's
		// how the HTTP requests come.
		h.m["/"+f.Name] = f
	}

	return h
}

func (h *fileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	upath := r.URL.Path
	if !strings.HasPrefix(upath, "/") {
		upath = "/" + upath
		r.URL.Path = upath
	}
	upath = path.Clean(upath)
	f, ok := h.m[upath]
	if !ok {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Etag", calcEtag(f))
	rangeReq, done := checkETag(w, r, f.ModTime())
	if done {
		return
	}
	if rangeReq != "" {
		// a range request has been requested, and for this we
		// allow the std library to handle it.
		serveStandard(w, r, f)
		return
	}

	// At this point we are prepared to serve the whole file
	// to the client, handle according to the compression method.
	switch f.Method {
	case zip.Store:
		serveIdentity(w, r, f)
	case zip.Deflate:
		serveDeflate(w, r, f, h.readerAt)
	default:
		http.Error(w, fmt.Sprintf("unsupported zip method: %d", f.Method), http.StatusInternalServerError)
	}
}

func serveIdentity(w http.ResponseWriter, r *http.Request, f *zip.File) {
	// TODO: need to check if the client explicitly refuses to accept
	// identity encoding (Accept-Encoding: identity;q=0), but this is
	// going to be very rare.

	reader, err := f.Open()
	if err != nil {
		internalServerError(w, r, err)
		return
	}
	defer reader.Close()

	setContentType(w, f.Name)
	w.Header().Del("Content-Encoding")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", f.UncompressedSize64))
	if r.Method != "HEAD" {
		io.CopyN(w, reader, int64(f.UncompressedSize64))
	}
}

func serveDeflate(w http.ResponseWriter, r *http.Request, f *zip.File, readerAt io.ReaderAt) {
	acceptEncoding := r.Header.Get("Accept-Encoding")

	// TODO: need to parse the accept header to work out if the
	// client is explicitly forbidding deflate (ie deflate;q=0)
	acceptsDeflate := strings.Contains(acceptEncoding, "deflate")
	if !acceptsDeflate {
		// client will not accept deflate, so serve as identity
		serveIdentity(w, r, f)
		return
	}

	setContentType(w, f.Name)
	w.Header().Set("Content-Encoding", "deflate")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", f.CompressedSize64))
	if r.Method == "HEAD" {
		return
	}

	var written int64
	remaining := int64(f.CompressedSize64)
	offset, err := f.DataOffset()
	if err != nil {
		internalServerError(w, r, err)
		return
	}

	// re-use buffers to reduce stress on GC
	buf := getBuf()
	defer freeBuf(buf)

	// loop to write the raw deflated content to the client
	for remaining > 0 {
		size := len(buf)
		if int64(size) > remaining {
			size = int(remaining)
		}

		// Note that we read into a different slice than was
		// obtained from getBuf. The reason for this is that
		// we want to be able to give back the original slice
		// so that it can be re-used.
		b := buf[:size]
		_, err := readerAt.ReadAt(b, offset)
		if err != nil {
			if written == 0 {
				// have not written anything to the client yet, so we can send an error
				internalServerError(w, r, err)
			}
			return
		}
		if _, err := w.Write(b); err != nil {
			// Cannot write an error to the client because, er,  we just
			// failed to write to the client.
			return
		}
		written += int64(size)
		remaining -= int64(size)
		offset += int64(size)
	}
}

func setContentType(w http.ResponseWriter, filename string) {
	ctypes, haveType := w.Header()["Content-Type"]
	var ctype string
	if !haveType {
		ctype = mime.TypeByExtension(filepath.Ext(path.Base(filename)))
		if ctype == "" {
			// the standard library sniffs content to decide whether it is
			// binary or text, but this requires a ReaderSeeker, and we
			// only have a reader from the zip file. Assume binary.
			ctype = "application/octet-stream"
		}
	} else if len(ctypes) > 0 {
		ctype = ctypes[0]
	}
	if ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
}

func calcEtag(f *zip.File) string {
	etag := uint64(f.CRC32) ^ (uint64(f.CompressedSize64&0xffffffff) << 32)

	// etag should always be in double quotes
	return fmt.Sprintf(`"%x"`, etag)
}

// serveStandard extracts the file from the zip file to a temporary
// location and serves it using the std library. This only happens
// for more complicated requests, such as range requests.
func serveStandard(w http.ResponseWriter, r *http.Request, f *zip.File) {
	tempFile, err := createTempFile(f)
	if err != nil {
		internalServerError(w, r, err)
		return
	}
	defer func() {
		tempFile.Close()
		os.Remove(tempFile.Name())
	}()

	http.ServeContent(w, r, f.Name, f.ModTime(), tempFile)
}

func createTempFile(f *zip.File) (*os.File, error) {
	reader, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	tempFile, err := ioutil.TempFile("", "zipfs")
	if err != nil {
		return nil, err
	}

	_, err = io.Copy(tempFile, reader)
	if err != nil {
		tempFile.Close()
		os.Remove(tempFile.Name())
		return nil, err
	}
	_, err = tempFile.Seek(0, os.SEEK_SET)
	if err != nil {
		tempFile.Close()
		os.Remove(tempFile.Name())
		return nil, err
	}

	return tempFile, nil
}

func internalServerError(w http.ResponseWriter, r *http.Request, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
